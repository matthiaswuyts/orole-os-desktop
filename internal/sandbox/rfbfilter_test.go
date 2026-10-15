package sandbox

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func handshake() []byte { return append([]byte("RFB 003.008\n"), 1, 0) }

func key(down byte) []byte { return []byte{4, down, 0, 0, 0, 0, 0xff, 0x0d} }

func encodings(list ...int32) []byte {
	b := []byte{2, 0, 0, 0}
	binary.BigEndian.PutUint16(b[2:], uint16(len(list)))
	for _, e := range list {
		b = binary.BigEndian.AppendUint32(b, uint32(e))
	}
	return b
}

func cutText(n int32, payload int) []byte {
	b := []byte{6, 0, 0, 0}
	b = binary.BigEndian.AppendUint32(b, uint32(n))
	return append(b, bytes.Repeat([]byte{'a'}, payload)...)
}

func TestRFBFilter(t *testing.T) {
	fbReq := []byte{3, 1, 0, 0, 0, 0, 0x04, 0x00, 0x03, 0x00}
	pointer := []byte{5, 1, 0, 10, 0, 20}
	qemuKey := append([]byte{255, 0, 0, 1}, make([]byte, 8)...)
	xvp := []byte{250, 0, 1, 2}
	desktopSize := append([]byte{251, 0, 4, 0, 3, 0, 1, 0}, make([]byte, 16)...)
	fence := []byte{248, 0, 0, 0, 0, 0, 0, 0, 2, 'h', 'i'}
	pixelFormat := append([]byte{0}, make([]byte, 19)...)
	wantHandshake := append([]byte("RFB 003.008\n"), 1, 1) // ClientInit forced to shared

	tests := []struct {
		name    string
		allow   bool
		in      [][]byte
		want    []byte
		wantErr bool
	}{
		{name: "handshake passes, client init forced shared", in: [][]byte{handshake()}, want: wantHandshake},
		{name: "wrong protocol version", in: [][]byte{[]byte("RFB 003.003\n")}, wantErr: true},
		{name: "security type other than None", in: [][]byte{append([]byte("RFB 003.008\n"), 2)}, wantErr: true},
		{name: "view messages pass while view-only", in: [][]byte{handshake(), fbReq, pixelFormat, fence, {150, 1, 0, 0, 0, 0, 4, 0, 3, 0}},
			want: concat(wantHandshake, fbReq, pixelFormat, fence, []byte{150, 1, 0, 0, 0, 0, 4, 0, 3, 0})},
		{name: "key, pointer, clipboard and qemu key dropped while view-only", in: [][]byte{handshake(), key(1), pointer, cutText(2, 2), qemuKey, fbReq},
			want: concat(wantHandshake, fbReq)},
		{name: "key, pointer, clipboard and qemu key pass after take over", allow: true, in: [][]byte{handshake(), key(1), pointer, cutText(2, 2), qemuKey},
			want: concat(wantHandshake, key(1), pointer, cutText(2, 2), qemuKey)},
		{name: "extended clipboard negative length", allow: true, in: [][]byte{handshake(), cutText(-3, 3)}, want: concat(wantHandshake, cutText(-3, 3))},
		{name: "clipboard over 1 MiB is refused", allow: true, in: [][]byte{handshake(), cutText(1<<20+1, 0)}, wantErr: true},
		{name: "xvp power actions always dropped", allow: true, in: [][]byte{handshake(), xvp, fbReq}, want: concat(wantHandshake, fbReq)},
		{name: "desktop resize always dropped", allow: true, in: [][]byte{handshake(), desktopSize, fbReq}, want: concat(wantHandshake, fbReq)},
		{name: "extended mouse buttons and xvp encodings stripped", in: [][]byte{handshake(), encodings(7, -316, 16, -309, -223)},
			want: concat(wantHandshake, encodings(7, 16, -223))},
		{name: "unknown message type", in: [][]byte{handshake(), {7, 0, 0, 0}}, wantErr: true},
		{name: "qemu message other than extended key", allow: true, in: [][]byte{handshake(), {255, 1, 0, 0}}, wantErr: true},
		{name: "too many encodings", in: [][]byte{handshake(), {2, 0, 0x10, 0x00}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newRFBFilter(func() bool { return tt.allow })
			var got []byte
			var err error
			for _, p := range tt.in {
				var out []byte
				out, err = f.Feed(p)
				if err != nil {
					break
				}
				got = append(got, out...)
			}
			if tt.wantErr {
				if !errors.Is(err, errRFBInput) {
					t.Fatalf("err = %v, want errRFBInput", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("forwarded\n %v\nwant\n %v", got, tt.want)
			}
		})
	}
}

func TestRFBFilterReassemblesMessagesSplitAcrossFeeds(t *testing.T) {
	allow := false
	f := newRFBFilter(func() bool { return allow })
	stream := concat(handshake(), key(1), []byte{3, 1, 0, 0, 0, 0, 4, 0, 3, 0})
	var got []byte
	for i := range stream { // one byte at a time
		out, err := f.Feed(stream[i : i+1])
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, out...)
	}
	want := concat(append([]byte("RFB 003.008\n"), 1, 1), []byte{3, 1, 0, 0, 0, 0, 4, 0, 3, 0})
	if !bytes.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	// The decision is taken when a message completes: a key that finishes after Take over passes.
	allow = true
	k := key(0)
	if out, _ := f.Feed(k[:3]); len(out) != 0 {
		t.Fatalf("partial message forwarded: %v", out)
	}
	if out, _ := f.Feed(k[3:]); !bytes.Equal(out, k) {
		t.Fatalf("completed key = %v", out)
	}
}

// TestRFBFilterSplitAtEveryByte feeds one mixed stream whole, in two parts split at every offset, and one byte at a
// time; the forwarded bytes must always equal the whole-stream result.
func TestRFBFilterSplitAtEveryByte(t *testing.T) {
	for _, allow := range []bool{false, true} {
		stream := concat(
			handshake(),
			[]byte{0}, make([]byte, 19), // SetPixelFormat
			encodings(7, -316, 16, -309, -223),
			[]byte{3, 1, 0, 0, 0, 0, 4, 0, 3, 0},
			key(1), []byte{5, 1, 0, 10, 0, 20},
			cutText(5, 5), cutText(-3, 3),
			append([]byte{255, 0, 0, 1}, make([]byte, 8)...),
			[]byte{250, 0, 1, 2},
			append([]byte{251, 0, 4, 0, 3, 0, 2, 0}, make([]byte, 32)...),
			[]byte{248, 0, 0, 0, 0, 0, 0, 0, 2, 'h', 'i'},
			[]byte{150, 1, 0, 0, 0, 0, 4, 0, 3, 0},
		)
		run := func(parts ...[]byte) []byte {
			f := newRFBFilter(func() bool { return allow })
			var got []byte
			for _, p := range parts {
				out, err := f.Feed(p)
				if err != nil {
					t.Fatalf("allow=%v: %v", allow, err)
				}
				got = append(got, out...)
			}
			return got
		}
		want := run(stream)
		if len(want) == 0 {
			t.Fatal("empty reference output")
		}
		for i := 0; i <= len(stream); i++ {
			if got := run(stream[:i], stream[i:]); !bytes.Equal(got, want) {
				t.Fatalf("allow=%v split at %d: got %v want %v", allow, i, got, want)
			}
		}
		var single [][]byte
		for i := range stream {
			single = append(single, stream[i:i+1])
		}
		if got := run(single...); !bytes.Equal(got, want) {
			t.Fatalf("allow=%v byte-wise: got %v want %v", allow, got, want)
		}
	}
}

// TestRFBFilterRefusesEncodingCountOverCap checks the SetEncodings count cap (257 > 256).
func TestRFBFilterRefusesEncodingCountOverCap(t *testing.T) {
	f := newRFBFilter(func() bool { return true })
	if _, err := f.Feed(handshake()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Feed([]byte{2, 0, 0x01, 0x01}); !errors.Is(err, errRFBInput) {
		t.Fatalf("err = %v, want errRFBInput", err)
	}
}

func concat(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}
