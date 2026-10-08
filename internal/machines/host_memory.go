package machines

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

// These probes accept no frontend paths, programs, arguments or script fragments.
const linuxMemoryProbe = `LC_ALL=C; export LC_ALL; /bin/hostname; /bin/uname -m; /bin/cat /proc/meminfo`
const windowsMemoryProbe = `$ErrorActionPreference='Stop'; $o=Get-CimInstance -ClassName Win32_OperatingSystem; [ordered]@{hostname=[Environment]::MachineName; architecture=$o.OSArchitecture; totalKiB=[UInt64]$o.TotalVisibleMemorySize; availableKiB=[UInt64]$o.FreePhysicalMemory} | ConvertTo-Json -Compress`

// probeOutput fails rather than retaining unbounded output from a paired host.
type probeOutput struct{ buffer bytes.Buffer }

func (b *probeOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 32768 {
		return 0, errors.New("host metric probe exceeded 32 KiB output limit")
	}
	return b.buffer.Write(p)
}

func isHostOffline(err error) bool {
	var network *net.OpError
	return errors.As(err, &network) && (network.Op == "dial" || network.Op == "read")
}

func readRemoteHostMemory(op *accessOperation, client *ssh.Client, h *MachineHealth) error {
	var command string
	var err error
	switch op.machine.Backend {
	case "posix":
		command, err = remoteCommand("posix", "/bin/sh", []string{"-c", linuxMemoryProbe})
	case "powershell":
		command, err = remoteCommand("powershell", `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", windowsMemoryProbe})
	default:
		return errors.New("host memory is unsupported for this execution backend")
	}
	if err != nil {
		return err
	}
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	var stdout, stderr probeOutput
	session.Stdout, session.Stderr = &stdout, &stderr
	unlock, err := op.guard()
	if err != nil {
		return err
	}
	err = session.Start(command)
	unlock()
	if err != nil {
		return err
	}
	if err = session.Wait(); err != nil {
		return fmt.Errorf("host memory probe failed: %w", err)
	}
	if err = op.ctx.Err(); err != nil {
		return err
	}
	return parseHostMemory(op.machine.Backend, stdout.buffer.String(), h)
}

func parseHostMemory(backend, output string, h *MachineHealth) error {
	var total, available uint64
	var hostname, architecture string
	if backend == "powershell" {
		var v struct {
			Hostname     string  `json:"hostname"`
			Architecture string  `json:"architecture"`
			TotalKiB     *uint64 `json:"totalKiB"`
			AvailableKiB *uint64 `json:"availableKiB"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(output, "\ufeff")), &v); err != nil {
			return fmt.Errorf("invalid Windows host metrics: %w", err)
		}
		if v.TotalKiB == nil || v.AvailableKiB == nil {
			return errors.New("incomplete Windows host metrics")
		}
		total, available, hostname, architecture = *v.TotalKiB, *v.AvailableKiB, v.Hostname, v.Architecture
	} else {
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) < 4 {
			return errors.New("incomplete Linux host metrics")
		}
		hostname, architecture = strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
		foundTotal, foundAvailable := false, false
		for _, line := range lines[2:] {
			fields := strings.Fields(line)
			if len(fields) == 0 || (fields[0] != "MemTotal:" && fields[0] != "MemAvailable:") {
				continue
			}
			if len(fields) != 3 || fields[2] != "kB" {
				return errors.New("invalid Linux memory units")
			}
			value, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return errors.New("invalid Linux memory counter")
			}
			if fields[0] == "MemTotal:" {
				if foundTotal {
					return errors.New("duplicate memory total")
				}
				total, foundTotal = value, true
			} else {
				if foundAvailable {
					return errors.New("duplicate available memory")
				}
				available, foundAvailable = value, true
			}
		}
		if !foundTotal || !foundAvailable {
			return errors.New("Linux MemTotal or MemAvailable is unavailable")
		}
	}
	if total == 0 || total > math.MaxUint64/1024 || available > total {
		return errors.New("invalid physical memory counters")
	}
	if hostname == "" || len(hostname) > 255 || len(architecture) > 128 || !approvalText(hostname) || !approvalText(architecture) {
		return errors.New("invalid host identity in memory probe")
	}
	h.Hostname, h.Architecture = hostname, architecture
	h.MemoryTotalBytes, h.MemoryAvailableBytes, h.MemoryReady = total*1024, available*1024, true
	return nil
}
