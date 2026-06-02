package compress

import (
	"bytes"
	"io"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestBrotliEncoder_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	enc := NewBrotliEncoder(&buf)
	want := []byte("hello, brotli — repeated " + repeatN("xyz", 200))
	if _, err := enc.Write(want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if buf.Len() >= len(want) {
		t.Fatalf("compressed (%d) >= original (%d) — likely not compressed", buf.Len(), len(want))
	}
	got, err := io.ReadAll(brotli.NewReader(&buf))
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("roundtrip mismatch:\nwant=%q\n got=%q", want, got)
	}
}

func repeatN(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
