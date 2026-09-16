package cachepool

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/itchio/lake/pools/fspool"
	"github.com/itchio/lake/tlc"
)

// oneShotSource dies on Close, like a pool that owns its archive handle.
type oneShotSource struct {
	files  [][]byte
	closed bool
}

func (p *oneShotSource) GetSize(i int64) int64 { return int64(len(p.files[i])) }
func (p *oneShotSource) GetReader(i int64) (io.Reader, error) {
	if p.closed {
		return nil, errors.New("source used after Close")
	}
	return bytes.NewReader(p.files[i]), nil
}
func (p *oneShotSource) GetReadSeeker(i int64) (io.ReadSeeker, error) {
	r, err := p.GetReader(i)
	if err != nil {
		return nil, err
	}
	return r.(io.ReadSeeker), nil
}
func (p *oneShotSource) Close() error {
	p.closed = true
	return nil
}

func TestPreloadLeavesSourceOpen(t *testing.T) {
	source := &oneShotSource{files: [][]byte{[]byte("first"), []byte("second")}}
	c := &tlc.Container{}
	for i, f := range source.files {
		c.Files = append(c.Files, &tlc.File{Path: filepath.Join("dir", string(rune('a'+i))), Size: int64(len(f))})
	}
	dir := t.TempDir()
	cp := New(c, source, fspool.New(c, dir))
	for i := range source.files {
		if err := cp.Preload(int64(i)); err != nil {
			t.Fatalf("preload %d: %v", i, err)
		}
	}
	for i, want := range source.files {
		r, err := cp.GetReadSeeker(int64(i))
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("file %d = %q, want %q", i, got, want)
		}
	}
	if source.closed {
		t.Fatal("source closed before cachepool.Close")
	}
	if err := cp.Close(); err != nil {
		t.Fatal(err)
	}
	if !source.closed {
		t.Fatal("cachepool.Close did not close the source")
	}
	if _, err := os.Stat(filepath.Join(dir, "dir", "b")); err != nil {
		t.Fatal(err)
	}
}
