//go:build windows

package credentials

import (
	"bytes"
	"testing"
)

func TestRoundTripAndDelete(t *testing.T) {
	s := New()
	k := "test/roundtrip"
	defer s.Delete(k)
	if _, err := s.Get(k); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.Set(k, []byte("s3cret")); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(k)
	if err != nil || !bytes.Equal(got, []byte("s3cret")) {
		t.Fatalf("got %q %v", got, err)
	}
	if err := s.Delete(k); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(k); err != ErrNotFound {
		t.Fatal("not deleted")
	}
}
