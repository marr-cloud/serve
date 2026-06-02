package handler

import (
	"testing"
	"testing/fstest"
	"time"
)

func TestGenerateETag(t *testing.T) {
	mod := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	a := &fstest.MapFile{Data: []byte("hello"), ModTime: mod}
	b := &fstest.MapFile{Data: []byte("hello"), ModTime: mod}
	if generateETag(infoFrom(a)) != generateETag(infoFrom(b)) {
		t.Fatal("same modtime+size should produce identical ETag")
	}

	c := &fstest.MapFile{Data: []byte("hello!"), ModTime: mod}
	if generateETag(infoFrom(a)) == generateETag(infoFrom(c)) {
		t.Fatal("different size should produce different ETag")
	}

	d := &fstest.MapFile{Data: []byte("hello"), ModTime: mod.Add(time.Second)}
	if generateETag(infoFrom(a)) == generateETag(infoFrom(d)) {
		t.Fatal("different modtime should produce different ETag")
	}
}

func infoFrom(mf *fstest.MapFile) fileInfo {
	return fileInfo{size: int64(len(mf.Data)), mod: mf.ModTime}
}

type fileInfo struct {
	size int64
	mod  time.Time
}

func (f fileInfo) Size() int64        { return f.size }
func (f fileInfo) ModTime() time.Time { return f.mod }
