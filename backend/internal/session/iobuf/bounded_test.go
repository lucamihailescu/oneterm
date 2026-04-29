package iobuf

import (
	"bytes"
	"strings"
	"testing"
)

func TestBoundedBuffer_EvictsOldestWhenFull(t *testing.T) {
	b := NewBoundedBuffer(8)
	b.Write([]byte("AAAAA"))
	b.Write([]byte("BBBBB"))

	got := b.Bytes()
	if len(got) > 8 {
		t.Fatalf("buffer exceeded cap: len=%d cap=8", len(got))
	}
	if !bytes.HasSuffix(got, []byte("BBBBB")) {
		t.Fatalf("expected newest bytes preserved, got %q", got)
	}
}

func TestBoundedBuffer_SingleWriteLargerThanCapTruncates(t *testing.T) {
	b := NewBoundedBuffer(4)
	b.Write([]byte("0123456789"))

	got := b.Bytes()
	if string(got) != "6789" {
		t.Fatalf("expected last 4 bytes %q, got %q", "6789", got)
	}
}

func TestBoundedBuffer_ResetClears(t *testing.T) {
	b := NewBoundedBuffer(16)
	b.WriteString(strings.Repeat("x", 20))
	b.Reset()

	if l := b.Len(); l != 0 {
		t.Fatalf("expected empty after Reset, got len=%d", l)
	}
}
