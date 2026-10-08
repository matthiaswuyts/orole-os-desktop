//go:build !windows

package systemmetrics

import (
	"fmt"
	"runtime"
)

func readHost(string) (hostReading, error) {
	return hostReading{}, fmt.Errorf("%w on %s: native host sampling is currently available only on Windows", ErrUnsupported, runtime.GOOS)
}
