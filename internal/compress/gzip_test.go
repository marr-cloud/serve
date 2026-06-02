package compress

import (
	"bytes"
	stdgzip "compress/gzip"
	"io"
	"testing"
)

func TestGzipEncoder(t *testing.T) {
	input := bytes.Repeat([]byte("hello world\n"), 100)

	var buf bytes.Buffer
	enc := NewGzipEncoder(&buf)
	if _, err := enc.Write(input); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if buf.Len() >= len(input) {
		t.Fatalf("compressed size %d not smaller than input %d", buf.Len(), len(input))
	}

	gr, err := stdgzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Fatal("roundtrip mismatch")
	}
}
