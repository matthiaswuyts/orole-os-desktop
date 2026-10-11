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

// Credential Manager refuses a blob over 2560 bytes; OAuth token sets (Cloudflare) are larger.
func TestSecretsLargerThanOneCredentialRoundTrip(t *testing.T) {
	s := New()
	k := "test/large"
	defer s.Delete(k)
	large := bytes.Repeat([]byte("0123456789abcdef"), 700) // 11200 bytes
	if err := s.Set(k, large); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get(k); err != nil || !bytes.Equal(got, large) {
		t.Fatalf("large secret not restored: %d bytes, %v", len(got), err)
	}
	// Shrinking to a plain secret must leave no stale chunks behind.
	if err := s.Set(k, []byte("small")); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get(k); err != nil || string(got) != "small" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := read(chunkKey(k, 0)); err != ErrNotFound {
		t.Fatalf("stale chunk survived: %v", err)
	}
	if err := s.Set(k, large); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(k); err != nil {
		t.Fatal(err)
	}
	if _, err := read(chunkKey(k, 0)); err != ErrNotFound {
		t.Fatalf("chunk survived Delete: %v", err)
	}
}
