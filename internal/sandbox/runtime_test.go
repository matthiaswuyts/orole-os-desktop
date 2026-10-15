package sandbox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pinOf(name string, data []byte) Asset {
	sum := sha256.Sum256(data)
	return Asset{Name: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
}

type fixture struct {
	pins   Pins
	served map[string][]byte // URL path -> bytes the server sends
	hits   atomic.Int32
	srv    *httptest.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{served: map[string][]byte{}}
	qemu := testZip(t, map[string]string{
		"qemu-system-x86_64.exe": "MZ qemu", "qemu-img.exe": "MZ img", "libglib-2.0-0.dll": "dll", "share/bios-256k.bin": "bios",
	})
	f.pins = Pins{QEMUVersion: "20260811", QEMUTag: "qemu-v20260811", QEMU: pinOf("qemu-20260811-win64.zip", qemu),
		ImageVersion: "0.1.0", ImageTag: "guest-v0.1.0"}
	f.served["/qemu-v20260811/qemu-20260811-win64.zip"] = qemu
	for name, body := range map[string]string{imageKernel: "kernel", imageInitrd: "initrd", imageCmdline: "init=/nix/store/x/init", imageBase: "qcow2 base"} {
		f.pins.Image = append(f.pins.Image, pinOf(name, []byte(body)))
		f.served["/guest-v0.1.0/"+name] = []byte(body)
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		b, ok := f.served[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) runtime(t *testing.T) (*Runtime, Paths) {
	paths := Paths{Root: t.TempDir()}
	return NewRuntime(RuntimeConfig{Paths: paths, Pins: f.pins, BaseURL: f.srv.URL}), paths
}

func noStaging(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".staging-") {
			t.Fatalf("staging folder left behind: %s", filepath.Join(dir, e.Name()))
		}
	}
}

func TestInstallVerifiesAndUnpacksQEMUAndTheImage(t *testing.T) {
	f := newFixture(t)
	rt, paths := f.runtime(t)
	if rt.Ready() {
		t.Fatal("ready before install")
	}
	if err := rt.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !rt.Ready() {
		t.Fatal("not ready after install")
	}
	q, err := rt.QEMU()
	if err != nil {
		t.Fatal(err)
	}
	if q.System != filepath.Join(paths.Runtime("20260811"), "qemu-system-x86_64.exe") || !pathExists(q.Img) || !pathExists(filepath.Join(q.Share, "bios-256k.bin")) {
		t.Fatalf("qemu files %+v", q)
	}
	img, err := rt.Image()
	if err != nil {
		t.Fatal(err)
	}
	if img.Version != "0.1.0" || img.Kernel != filepath.Join(paths.Image("0.1.0"), imageKernel) || !pathExists(img.BaseImage) || !pathExists(img.Initrd) {
		t.Fatalf("image %+v", img)
	}
	if b, _ := os.ReadFile(filepath.Join(paths.Image("0.1.0"), imageCmdline)); string(b) != "init=/nix/store/x/init" {
		t.Fatalf("cmdline %q", b)
	}
	noStaging(t, filepath.Join(paths.Root, "runtime"))
	noStaging(t, filepath.Join(paths.Root, "images"))
}

func TestInstallRejectsAHashMismatchAndLeavesNothingBehind(t *testing.T) {
	f := newFixture(t)
	f.served["/guest-v0.1.0/"+imageBase] = []byte("qcow2 bASE") // same size, different bytes
	rt, paths := f.runtime(t)
	err := rt.Install(context.Background())
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("err = %v", err)
	}
	if rt.Ready() || pathExists(paths.Image("0.1.0")) {
		t.Fatal("a mismatched image was installed")
	}
	noStaging(t, filepath.Join(paths.Root, "images"))
	f.served["/guest-v0.1.0/"+imageBase] = []byte("qcow2 base") // the retry starts clean and succeeds
	if err := rt.Install(context.Background()); err != nil || !rt.Ready() {
		t.Fatalf("retry: %v", err)
	}
}

func TestInstallRejectsATruncatedDownload(t *testing.T) {
	f := newFixture(t)
	f.served["/qemu-v20260811/qemu-20260811-win64.zip"] = f.served["/qemu-v20260811/qemu-20260811-win64.zip"][:20]
	rt, paths := f.runtime(t)
	if err := rt.Install(context.Background()); err == nil || !strings.Contains(err.Error(), "bytes") {
		t.Fatalf("err = %v", err)
	}
	if pathExists(paths.Runtime("20260811")) {
		t.Fatal("a truncated QEMU was installed")
	}
	noStaging(t, filepath.Join(paths.Root, "runtime"))
}

func TestInstallReportsAMissingAsset(t *testing.T) {
	f := newFixture(t)
	delete(f.served, "/guest-v0.1.0/"+imageInitrd)
	rt, _ := f.runtime(t)
	if err := rt.Install(context.Background()); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v", err)
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	f := newFixture(t)
	rt, _ := f.runtime(t)
	if err := rt.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := f.hits.Load()
	if err := rt.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.hits.Load() != before {
		t.Fatal("a second install downloaded again")
	}
}

func TestInstallRefusesUnsafeZipPaths(t *testing.T) {
	f := newFixture(t)
	evil := testZip(t, map[string]string{"qemu-system-x86_64.exe": "x", `..\evil.dll`: "x"})
	f.served["/qemu-v20260811/qemu-20260811-win64.zip"] = evil
	f.pins.QEMU = pinOf("qemu-20260811-win64.zip", evil)
	rt, paths := f.runtime(t)
	if err := rt.Install(context.Background()); err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("err = %v", err)
	}
	if pathExists(filepath.Join(paths.Root, "evil.dll")) || pathExists(filepath.Join(paths.Root, "runtime", "evil.dll")) {
		t.Fatal("zip slip")
	}
}

func TestInstallWithoutPinsSaysSo(t *testing.T) {
	rt := NewRuntime(RuntimeConfig{Paths: Paths{Root: t.TempDir()}})
	if err := rt.Install(context.Background()); !errors.Is(err, ErrNoPins) {
		t.Fatalf("err = %v", err)
	}
	if rt.Ready() {
		t.Fatal("ready without pins")
	}
	if _, err := rt.Image(); !errors.Is(err, ErrNotReady) {
		t.Fatalf("Image err = %v", err)
	}
}

func TestStartRunsOneInstallAtATimeAndReportsProgress(t *testing.T) {
	f := newFixture(t)
	paths := Paths{Root: t.TempDir()}
	var events atomic.Int32
	rt := NewRuntime(RuntimeConfig{Paths: paths, Pins: f.pins, BaseURL: f.srv.URL, OnChange: func(RuntimeState) { events.Add(1) }})
	first := rt.Start(context.Background())
	second := rt.Start(context.Background())
	if !first.Installing || !second.Installing {
		t.Fatalf("states %+v %+v", first, second)
	}
	deadline := time.Now().Add(10 * time.Second)
	for rt.State().Installing && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	st := rt.State()
	if st.Installing || !st.Ready || st.Error != "" {
		t.Fatalf("final state %+v", st)
	}
	if got := f.hits.Load(); got != int32(1+len(f.pins.Image)) {
		t.Fatalf("%d downloads, want %d (one install)", got, 1+len(f.pins.Image))
	}
	if events.Load() < 2 {
		t.Fatal("no progress events")
	}
}

func TestInstallKeepsAnOlderImageVersionOnDisk(t *testing.T) {
	f := newFixture(t)
	rt, paths := f.runtime(t)
	old := filepath.Join(paths.Image("0.0.9"), imageBase)
	if err := os.MkdirAll(filepath.Dir(old), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old base"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rt.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !pathExists(old) {
		t.Fatal("installing a new image version removed an older one that overlays may still use")
	}
}
