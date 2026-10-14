package cachepool

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// blockingSource holds every read until released, like a stalled remote
// archive. started receives one value per read that entered GetReader.
type blockingSource struct {
	files   [][]byte
	release chan struct{}
	started chan struct{}
}

func (p *blockingSource) GetSize(i int64) int64 { return int64(len(p.files[i])) }
func (p *blockingSource) GetReader(i int64) (io.Reader, error) {
	if p.started != nil {
		p.started <- struct{}{}
	}
	<-p.release
	return bytes.NewReader(p.files[i]), nil
}
func (p *blockingSource) GetReadSeeker(i int64) (io.ReadSeeker, error) {
	r, err := p.GetReader(i)
	if err != nil {
		return nil, err
	}
	return r.(io.ReadSeeker), nil
}
func (p *blockingSource) Close() error { return nil }

func TestAbortWakesWaitersAndKeepsPoolsOpen(t *testing.T) {
	source := &blockingSource{files: [][]byte{[]byte("ready"), []byte("never")}, release: make(chan struct{})}
	c := &tlc.Container{Files: []*tlc.File{{Path: "a", Size: 5}, {Path: "b", Size: 5}}}
	dir := t.TempDir()
	cp := New(c, source, fspool.New(c, dir))

	// File 0 is preloaded; file 1 never will be.
	close(source.release)
	if err := cp.Preload(0); err != nil {
		t.Fatal(err)
	}
	source.release = make(chan struct{})

	waited := make(chan error, 1)
	go func() {
		_, err := cp.GetReadSeeker(1)
		waited <- err
	}()

	// A reader already handed out keeps working through and after Abort,
	// which is only true if Abort leaves the cache open.
	held, err := cp.GetReadSeeker(0)
	if err != nil {
		t.Fatal(err)
	}
	readers := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(held)
		readers <- err
	}()

	cause := errors.New("job cancelled")
	cp.Abort(cause)

	select {
	case err := <-waited:
		if !errors.Is(err, cause) {
			t.Fatalf("waiter got %v, want cause %v", err, cause)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Abort did not wake the waiting reader")
	}
	if err := <-readers; err != nil {
		t.Fatalf("held reader failed across Abort: %v", err)
	}
	if _, err := cp.GetReadSeeker(0); !errors.Is(err, cause) {
		t.Fatalf("read after Abort = %v, want cause %v", err, cause)
	}
	if err := cp.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAbortDuringActivePreload(t *testing.T) {
	source := &blockingSource{files: [][]byte{[]byte("slow")}, release: make(chan struct{}), started: make(chan struct{}, 1)}
	c := &tlc.Container{Files: []*tlc.File{{Path: "a", Size: 4}}}
	cp := New(c, source, fspool.New(c, t.TempDir()))

	preloaded := make(chan error, 1)
	go func() { preloaded <- cp.Preload(0) }()
	<-source.started

	cause := errors.New("job cancelled")
	cp.Abort(cause)
	close(source.release)

	select {
	case err := <-preloaded:
		if err != nil {
			t.Fatalf("preload after abort: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("preload did not finish")
	}
	if _, err := cp.GetReader(0); !errors.Is(err, cause) {
		t.Fatalf("read after abort = %v, want cause", err)
	}
}
