package pools_test

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/itchio/lake/pools"
	"github.com/itchio/lake/pools/zippool"
	"github.com/itchio/lake/tlc"
)

func TestNewWithZipOptions(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "game.zip")
	file, err := os.Create(archivePath)
	must(t, err)
	zw := zip.NewWriter(file)
	w, err := zw.Create("game")
	must(t, err)
	_, err = io.WriteString(w, "game contents")
	must(t, err)
	must(t, zw.Close())
	must(t, file.Close())
	container, err := tlc.WalkAny(archivePath, tlc.WalkOpts{})
	must(t, err)
	scratch := t.TempDir()
	pool, err := pools.NewWithOptions(container, archivePath, pools.Options{
		Zip: zippool.Options{MaxMemory: 4, TempDir: scratch},
	})
	must(t, err)
	defer pool.Close()
	r, err := pool.GetReadSeeker(0)
	must(t, err)
	_, err = r.Seek(-8, io.SeekEnd)
	must(t, err)
	data, err := io.ReadAll(r)
	must(t, err)
	if string(data) != "contents" {
		t.Fatalf("read %q", data)
	}
	entries, err := os.ReadDir(scratch)
	must(t, err)
	if len(entries) != 1 {
		t.Fatalf("expected a spilled entry, found %d", len(entries))
	}
	must(t, pool.Close())
	entries, err = os.ReadDir(scratch)
	must(t, err)
	if len(entries) != 0 {
		t.Fatalf("temp files remain: %v", entries)
	}
}

func TestZipOptionsIgnoredForFilesystem(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "game")
	must(t, os.WriteFile(path, []byte("game contents"), 0600))
	for _, src := range []string{root, path} {
		t.Run(filepath.Base(src), func(t *testing.T) {
			container, err := tlc.WalkAny(src, tlc.WalkOpts{})
			must(t, err)
			pool, err := pools.NewWithOptions(container, src, pools.Options{
				Zip: zippool.Options{MaxMemory: 1, TempDir: filepath.Join(root, "does-not-exist")},
			})
			must(t, err)
			defer pool.Close()
			r, err := pool.GetReadSeeker(0)
			must(t, err)
			data, err := io.ReadAll(r)
			must(t, err)
			if string(data) != "game contents" {
				t.Fatalf("read %q", data)
			}
		})
	}
}
