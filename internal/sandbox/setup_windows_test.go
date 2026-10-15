//go:build windows

package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

func TestShellExecuteInfoMatchesTheWin64Layout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) == 8 && unsafe.Sizeof(shellExecuteInfo{}) != 112 {
		t.Fatalf("sizeof(SHELLEXECUTEINFOW) = %d, want 112", unsafe.Sizeof(shellExecuteInfo{}))
	}
}

func TestEnableWHPWithoutTheHelperSaysSo(t *testing.T) {
	_, err := EnableWHP(context.Background(), filepath.Join(t.TempDir(), "orole-sandbox-setup.exe"), Paths{Root: t.TempDir()})
	if !errors.Is(err, ErrSetupHelperMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestEnableWHPLaunchesWithTheOneArgAndMarksReboot(t *testing.T) {
	helper := filepath.Join(t.TempDir(), "orole-sandbox-setup.exe")
	if err := os.WriteFile(helper, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	pinHelper(t, []byte("x"))
	cases := []struct {
		code       uint32
		err        error
		wantMarker bool
		wantErr    error
	}{
		{0, nil, false, nil},
		{3010, nil, true, nil},
		{0, ErrSetupDeclined, false, ErrSetupDeclined},
	}
	for _, c := range cases {
		p := Paths{Root: filepath.Join(t.TempDir(), "sandbox")}
		old := runElevatedFn
		var gotExe, gotParams string
		runElevatedFn = func(_ context.Context, exe, params string) (uint32, error) {
			gotExe, gotParams = exe, params
			return c.code, c.err
		}
		res, err := EnableWHP(context.Background(), helper, p)
		runElevatedFn = old
		if !errors.Is(err, c.wantErr) {
			t.Errorf("code %d: err = %v", c.code, err)
		}
		if gotExe != helper || gotParams != "enable-whp" {
			t.Errorf("launched %q %q", gotExe, gotParams)
		}
		if res.RebootRequired != c.wantMarker {
			t.Errorf("code %d: result %+v", c.code, res)
		}
		_, statErr := os.Stat(rebootMarkerPath(p))
		if (statErr == nil) != c.wantMarker {
			t.Errorf("code %d: marker present = %v", c.code, statErr == nil)
		}
	}
}

// pinHelper sets the embedded helper hash to content's for the test.
func pinHelper(t *testing.T, content []byte) {
	t.Helper()
	sum := sha256.Sum256(content)
	old := SetupHelperSHA256
	SetupHelperSHA256 = hex.EncodeToString(sum[:])
	t.Cleanup(func() { SetupHelperSHA256 = old })
}

func TestEnableWHPRefusesAnUnverifiedHelper(t *testing.T) {
	helper := filepath.Join(t.TempDir(), "orole-sandbox-setup.exe")
	if err := os.WriteFile(helper, []byte("replaced by something else"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		pin      []byte // nil: no embedded hash
		override string
		wantErr  error
		launched bool
	}{
		{"mismatch", []byte("the real helper"), "", ErrSetupHelperMismatch, false},
		{"mismatch with the dev override", []byte("the real helper"), "1", ErrSetupHelperMismatch, false},
		{"no embedded hash", nil, "", ErrSetupHelperUnverified, false},
		{"no embedded hash with the dev override", nil, "1", nil, true},
		{"match", []byte("replaced by something else"), "", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.pin != nil {
				pinHelper(t, c.pin)
			} else {
				old := SetupHelperSHA256
				SetupHelperSHA256 = ""
				t.Cleanup(func() { SetupHelperSHA256 = old })
			}
			t.Setenv(SetupHelperUnverifiedEnv, c.override)
			old := runElevatedFn
			t.Cleanup(func() { runElevatedFn = old })
			launched := false
			runElevatedFn = func(context.Context, string, string) (uint32, error) { launched = true; return 0, nil }
			_, err := EnableWHP(context.Background(), helper, Paths{Root: t.TempDir()})
			if !errors.Is(err, c.wantErr) || (c.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if launched != c.launched {
				t.Fatalf("launched = %v", launched)
			}
		})
	}
}

func TestEnableWHPKeepsTheVerifiedHelperFromBeingSwapped(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "orole-sandbox-setup.exe")
	if err := os.WriteFile(helper, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	pinHelper(t, []byte("x"))
	old := runElevatedFn
	t.Cleanup(func() { runElevatedFn = old })
	runElevatedFn = func(context.Context, string, string) (uint32, error) {
		// Between the check and the elevated start, nothing may change what runs as administrator.
		if err := os.WriteFile(helper, []byte("evil"), 0o600); err == nil {
			t.Error("the helper could be rewritten after it was verified")
		}
		if err := os.Rename(helper, filepath.Join(dir, "moved.exe")); err == nil {
			t.Error("the helper could be renamed after it was verified")
		}
		if err := os.Remove(helper); err == nil {
			t.Error("the helper could be deleted after it was verified")
		}
		if b, err := os.ReadFile(helper); err != nil || string(b) != "x" { // others may still read it (CreateProcess does)
			t.Errorf("read during launch: %q %v", b, err)
		}
		return 0, nil
	}
	if _, err := EnableWHP(context.Background(), helper, Paths{Root: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("y"), 0o600); err != nil { // released afterwards
		t.Fatalf("still locked after the launch: %v", err)
	}
}
