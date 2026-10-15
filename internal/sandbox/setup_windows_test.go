//go:build windows

package sandbox

import (
	"context"
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
