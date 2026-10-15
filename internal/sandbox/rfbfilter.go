package sandbox

import (
	"encoding/binary"
	"errors"
)

// errRFBInput is returned for screen input outside the RFB subset Orole accepts; the caller closes the stream.
var errRFBInput = errors.New("sandbox: unexpected screen input")

const (
	rfbMaxCutText   = 1 << 20 // largest clipboard text accepted from the user
	rfbMaxEncodings = 256
	rfbMaxScreens   = 16
	rfbMaxPending   = 8 + rfbMaxCutText

	encExtendedMouseButtons = -316 // makes PointerEvent 7 bytes; stripped so every PointerEvent is 6
	encXvp                  = -309 // lets the client power off, reboot or reset the guest; stripped
)

// rfbFilter checks the client half of an RFB 3.8 session (noVNC -> the guest's VNC server) before it reaches the
// guest. The daemon, not the UI, decides whether the user may type into or click on the bot's computer: keyboard,
// pointer, clipboard and QEMU key messages pass only while allowInput reports true and are dropped otherwise.
// Power and resize messages are always dropped. Anything outside this subset is errRFBInput.
type rfbFilter struct {
	allowInput func() bool
	stage      int    // 0 version, 1 security type, 2 client init, 3 normal messages
	buf        []byte // an incomplete message
}

func newRFBFilter(allowInput func() bool) *rfbFilter { return &rfbFilter{allowInput: allowInput} }

// Feed takes the next client bytes and returns those to forward to the guest. Complete messages are judged when they
// complete; a partial message waits in the filter.
func (f *rfbFilter) Feed(p []byte) ([]byte, error) {
	f.buf = append(f.buf, p...)
	var out []byte
	for {
		n, forward, err := f.next()
		if err != nil {
			f.buf = nil
			return nil, err
		}
		if n == 0 {
			if len(f.buf) > rfbMaxPending {
				f.buf = nil
				return nil, errRFBInput
			}
			if len(f.buf) == 0 {
				f.buf = nil
			}
			return out, nil
		}
		out = append(out, forward...)
		f.buf = f.buf[n:]
	}
}

// next parses one message from f.buf. n == 0 means more bytes are needed; forward is nil for a dropped message.
func (f *rfbFilter) next() (n int, forward []byte, err error) {
	b := f.buf
	switch f.stage {
	case 0:
		if len(b) < 12 {
			return 0, nil, nil
		}
		if string(b[:12]) != "RFB 003.008\n" {
			return 0, nil, errRFBInput
		}
		f.stage = 1
		return 12, b[:12], nil
	case 1:
		if len(b) < 1 {
			return 0, nil, nil
		}
		if b[0] != 1 { // security type None; the guest server listens on its own loopback only
			return 0, nil, errRFBInput
		}
		f.stage = 2
		return 1, b[:1], nil
	case 2:
		if len(b) < 1 {
			return 0, nil, nil
		}
		f.stage = 3
		return 1, []byte{1}, nil // ClientInit: always shared, so a second viewer never disconnects the first
	}
	if len(b) == 0 {
		return 0, nil, nil
	}
	fixed := func(size int) (int, []byte, error) {
		if len(b) < size {
			return 0, nil, nil
		}
		return size, b[:size], nil
	}
	drop := func(size int) (int, []byte, error) {
		if len(b) < size {
			return 0, nil, nil
		}
		return size, nil, nil
	}
	switch b[0] {
	case 0: // SetPixelFormat
		return fixed(20)
	case 2: // SetEncodings
		if len(b) < 4 {
			return 0, nil, nil
		}
		count := int(binary.BigEndian.Uint16(b[2:4]))
		if count > rfbMaxEncodings {
			return 0, nil, errRFBInput
		}
		size := 4 + 4*count
		if len(b) < size {
			return 0, nil, nil
		}
		return size, stripEncodings(b[:size]), nil
	case 3, 150: // FramebufferUpdateRequest, EnableContinuousUpdates
		return fixed(10)
	case 248: // ClientFence
		if len(b) < 9 {
			return 0, nil, nil
		}
		return fixed(9 + int(b[8]))
	case 4: // KeyEvent
		return f.input(8)
	case 5: // PointerEvent
		return f.input(6)
	case 6: // ClientCutText; a negative length is the extended clipboard format
		if len(b) < 8 {
			return 0, nil, nil
		}
		length := int64(int32(binary.BigEndian.Uint32(b[4:8])))
		if length < 0 {
			length = -length
		}
		if length > rfbMaxCutText {
			return 0, nil, errRFBInput
		}
		return f.input(8 + int(length))
	case 255: // QEMU client message: only the extended key event
		if len(b) < 2 {
			return 0, nil, nil
		}
		if b[1] != 0 {
			return 0, nil, errRFBInput
		}
		return f.input(12)
	case 250: // xvp: power off, reboot, reset
		return drop(4)
	case 251: // SetDesktopSize: the bot's screen size is not the viewer's to change
		if len(b) < 8 {
			return 0, nil, nil
		}
		screens := int(b[6])
		if screens > rfbMaxScreens {
			return 0, nil, errRFBInput
		}
		return drop(8 + 16*screens)
	default:
		return 0, nil, errRFBInput
	}
}

// input forwards a complete input message only while the user holds the takeover lock.
func (f *rfbFilter) input(size int) (int, []byte, error) {
	if len(f.buf) < size {
		return 0, nil, nil
	}
	if f.allowInput() {
		return size, f.buf[:size], nil
	}
	return size, nil, nil
}

// stripEncodings removes pseudo-encodings that would change message sizes or allow power actions.
func stripEncodings(msg []byte) []byte {
	out := make([]byte, 4, len(msg))
	copy(out, msg[:4])
	for i := 4; i+4 <= len(msg); i += 4 {
		switch int32(binary.BigEndian.Uint32(msg[i : i+4])) {
		case encExtendedMouseButtons, encXvp:
			continue
		}
		out = append(out, msg[i:i+4]...)
	}
	binary.BigEndian.PutUint16(out[2:4], uint16((len(out)-4)/4))
	return out
}
