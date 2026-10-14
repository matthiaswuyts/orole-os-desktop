//go:build !windows || !amd64

package engine

import (
	"io"

	"github.com/rayorole/orole-os-desktop/internal/dictation"
)

// openNative: the speech engine is only built for Windows on x64 (dictation.EngineSupported).
func openNative(dictation.Spec, io.Writer) (backend, error) {
	return nil, dictation.Fail(dictation.CodeUnsupported, "Dictation is not available on this computer.")
}
