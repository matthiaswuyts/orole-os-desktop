//go:build linux

package main

import (
	"errors"
	"os"
	"strings"
)

// fwCfgTokenPath is where the host's -fw_cfg name=opt/orole/token,file=... appears once the qemu_fw_cfg module
// is loaded. The kernel makes it readable by root only (0400), and it is never printed, unlike the command line.
const fwCfgTokenPath = "/sys/firmware/qemu_fw_cfg/by_name/opt/orole/token/raw"

// fwCfgToken reads the token from a fw_cfg file; a missing or empty file is an error.
func fwCfgToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", errors.New("fw_cfg token is empty")
	}
	return tok, nil
}
