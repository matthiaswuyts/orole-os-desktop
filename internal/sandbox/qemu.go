package sandbox

import (
	"strconv"
	"strings"
	"sync"
)

// guestPortName is the virtio-serial port orole-guest opens as /dev/virtio-ports/<name>.
const guestPortName = "org.orole.guest.0"

// defaultCPU is QEMU's -cpu model under WHPX; the phase 2 spike (docs/architecture/bot-computer.md) chose it.
const defaultCPU = "host"

// defaultAccel is the QEMU accelerator on Windows. The phase 2 spike may fall back to "tcg" via qemuConfig.Accel.
const defaultAccel = "whpx"

// qemuConfig is everything one QEMU command line needs.
type qemuConfig struct {
	Spec        BootSpec
	Share       string // QEMU firmware folder (-L), <runtime>\share
	CPU         string // -cpu model
	Accel       string // -accel value; empty means defaultAccel (whpx). "tcg" is the software fallback
	Cmdline     string // the image's kernel command line (the cmdline file next to the kernel)
	GuestSocket string // AF_UNIX path the virtio-serial chardev connects to (the daemon listens)
	QMPSocket   string // AF_UNIX path the QMP monitor connects to (the daemon listens)
}

// qemuArgs builds the QEMU command line for one bot VM. QEMU only ever connects out to sockets the
// daemon listens on; it opens no listener of its own. The token travels in fw_cfg, never on the
// kernel command line (the guest console, which is logged, prints that).
func qemuArgs(c qemuConfig) []string {
	s := c.Spec
	accel := c.Accel
	if accel == "" {
		accel = defaultAccel
	}
	appendLine := strings.Join(strings.Fields(c.Cmdline+" console=ttyS0 panic=-1"), " ")
	return []string{
		"-nodefaults", "-no-user-config", "-no-reboot",
		"-name", qemuOpt("orole-" + s.BotID),
		"-machine", "q35", "-accel", accel, "-cpu", c.CPU,
		"-m", strconv.Itoa(s.Limits.MemoryMB), "-smp", strconv.Itoa(s.Limits.CPUs),
		"-L", c.Share,
		"-display", "none", "-vga", "none",
		"-kernel", s.Kernel, "-initrd", s.Initrd, "-append", appendLine,
		"-fw_cfg", "name=opt/orole/token,string=" + qemuOpt(s.Token),
		"-drive", "if=none,id=root,format=qcow2,cache=writeback,discard=unmap,file=" + qemuOpt(s.Overlay),
		"-device", "virtio-blk-pci,drive=root",
		"-netdev", "user,id=net0",
		"-device", "virtio-net-pci,netdev=net0,romfile=",
		"-device", "virtio-rng-pci",
		"-device", "virtio-serial-pci,id=vser",
		"-chardev", "socket,id=guest,path=" + qemuOpt(c.GuestSocket),
		"-device", "virtserialport,bus=vser.0,chardev=guest,name=" + guestPortName,
		"-chardev", "socket,id=qmp,path=" + qemuOpt(c.QMPSocket),
		"-mon", "chardev=qmp,mode=control",
		"-chardev", "file,id=con,append=on,path=" + qemuOpt(s.LogPath),
		"-serial", "chardev:con",
	}
}

// qemuOpt escapes a value inside a QEMU option list, where a comma is written as two commas.
func qemuOpt(v string) string { return strings.ReplaceAll(v, ",", ",,") }

// qmpSocket is the QMP socket path that belongs to a guest socket path.
func qmpSocket(guestSocket string) string { return strings.TrimSuffix(guestSocket, ".sock") + ".qmp" }

// redactArgs returns a copy of args with secret replaced, for logging.
func redactArgs(args []string, secret string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if secret != "" {
			a = strings.ReplaceAll(a, secret, "<redacted>")
		}
		out[i] = a
	}
	return out
}

// tailBuffer keeps the last max bytes written to it (QEMU's stdout and stderr), with secret removed.
type tailBuffer struct {
	mu     sync.Mutex
	max    int
	secret string
	b      []byte
}

func newTailBuffer(max int, secret string) *tailBuffer { return &tailBuffer{max: max, secret: secret} }

func (t *tailBuffer) Write(p []byte) (int, error) {
	chunk := p
	if t.secret != "" {
		chunk = []byte(strings.ReplaceAll(string(p), t.secret, "<redacted>"))
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, chunk...)
	if len(t.b) > t.max {
		t.b = append([]byte(nil), t.b[len(t.b)-t.max:]...)
	}
	return len(p), nil
}

// suffix is ": <tail>" for an error message, or "" when QEMU printed nothing.
func (t *tailBuffer) suffix() string {
	t.mu.Lock()
	s := strings.TrimSpace(string(t.b))
	t.mu.Unlock()
	if t.secret != "" {
		s = strings.ReplaceAll(s, t.secret, "<redacted>")
	}
	if s == "" {
		return ""
	}
	return ": " + s
}
