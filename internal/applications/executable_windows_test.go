package applications

import "testing"

func TestWindowsNativeExecutableBoundaries(t *testing.T) {
	for _, path := range []string{`C:\Apps\editor.exe`, `D:\Program Files\Editor.EXE`} {
		if !nativeExecutable(path, 0) {
			t.Errorf("rejected local executable %q", path)
		}
	}
	for _, path := range []string{`editor.exe`, `C:editor.exe`, `\\host\share\editor.exe`, `\\?\C:\Apps\editor.exe`, `\\.\pipe\editor.exe`, `C:\Apps\file:stream.exe`, `C:\Apps\launch.cmd`, `C:\Apps\launch.bat`, `C:\Apps\launch.ps1`, `C:\Apps\app.lnk`} {
		if nativeExecutable(path, 0) {
			t.Errorf("accepted unsupported path %q", path)
		}
	}
}
