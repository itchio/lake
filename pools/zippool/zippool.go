package zippool

import (
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/itchio/arkive/zip"

	"github.com/itchio/lake"
	"github.com/itchio/lake/tlc"
	"github.com/pkg/errors"
)

var verboseZipPool = os.Getenv("VERBOSE_ZIP_POOL") == "1"

// ZipPool implements the lake.ZipPool interface based on a Container
type ZipPool struct {
	options   Options
	container *tlc.Container
	fmap      map[string]*zip.File

	fileIndex int64
	reader    io.ReadCloser

	seekFileIndex int64
	readSeeker    ReadCloseSeeker
}

var _ lake.Pool = (*ZipPool)(nil)

// ReadCloseSeeker unifies io.Reader, io.Seeker, and io.Closer
type ReadCloseSeeker interface {
	io.Reader
	io.Seeker
	io.Closer
}

type Options struct {
	// MaxMemory caps how much of a seekable entry is held in memory before
	// the rest spills to a temp file. Zero means DefaultMaxMemory.
	MaxMemory int64
	// TempDir receives spilled entries and must already exist. Empty uses
	// os.TempDir, which on Linux is often tmpfs and so memory again.
	TempDir string
}

func New(c *tlc.Container, zipReader *zip.Reader) *ZipPool {
	return NewWithOptions(c, zipReader, Options{})
}

// NewWithOptions controls where seekable entries are spooled. Entries are
// decompressed only as far as reads demand. The spool is dropped when another
// seekable entry is requested or the pool is closed; the pool stays usable
// after Close. The caller owns the archive backing zipReader and must keep it
// open.
func NewWithOptions(c *tlc.Container, zipReader *zip.Reader, opts Options) *ZipPool {
	fmap := make(map[string]*zip.File)
	for _, f := range zipReader.File {
		info := f.FileInfo()

		if info.IsDir() {
			// muffin
		} else if (info.Mode() & os.ModeSymlink) > 0 {
			// muffin ether
		} else {
			// Normalize to match the path format produced by tlc.WalkZip
			key := path.Clean(strings.ReplaceAll(f.Name, `\`, `/`))
			fmap[key] = f
		}
	}

	return &ZipPool{
		options:   opts,
		container: c,
		fmap:      fmap,

		fileIndex: int64(-1),
		reader:    nil,

		seekFileIndex: int64(-1),
		readSeeker:    nil,
	}
}

// GetSize returns the size of the file at index fileIndex
func (cfp *ZipPool) GetSize(fileIndex int64) int64 {
	return cfp.container.Files[fileIndex].Size
}

// GetRelativePath returns the slashed path of a file, relative to
// the container's root.
func (cfp *ZipPool) GetRelativePath(fileIndex int64) string {
	return cfp.container.Files[fileIndex].Path
}

// GetPath returns the native path of a file (with slashes or backslashes)
// on-disk, based on the ZipPool's base path
func (cfp *ZipPool) GetPath(fileIndex int64) string {
	panic("ZipPool does not support GetPath")
}

// GetReader returns an io.Reader for the file at index fileIndex
// Successive calls to `GetReader` will attempt to re-use the last
// returned reader if the file index is similar. The cache size is 1, so
// reading in parallel from different files is not supported.
func (cfp *ZipPool) GetReader(fileIndex int64) (io.Reader, error) {
	if cfp.fileIndex != fileIndex {
		if cfp.reader != nil {
			cfp.fileIndex = -1
			err := cfp.reader.Close()
			if err != nil {
				return nil, errors.WithStack(err)
			}
			cfp.reader = nil
		}

		relPath := cfp.GetRelativePath(fileIndex)
		f := cfp.fmap[relPath]
		if f == nil {
			if verboseZipPool {
				fmt.Printf("\nzip contents:\n")
				for k := range cfp.fmap {
					fmt.Printf("\n- %s", k)
				}
				fmt.Println()
			}
			return nil, errors.WithStack(errors.Errorf("file not found in zip: %s", relPath))
		}

		reader, err := f.Open()

		if err != nil {
			return nil, errors.WithStack(err)
		}
		cfp.reader = reader
		cfp.fileIndex = fileIndex
	}

	return cfp.reader, nil
}

// GetReadSeeker is like GetReader but the returned object allows seeking
func (cfp *ZipPool) GetReadSeeker(fileIndex int64) (io.ReadSeeker, error) {
	if cfp.seekFileIndex != fileIndex {
		if cfp.readSeeker != nil {
			cfp.seekFileIndex = -1
			err := cfp.readSeeker.Close()
			if err != nil {
				return nil, errors.WithStack(err)
			}
			cfp.readSeeker = nil
		}

		key := cfp.GetRelativePath(fileIndex)
		f := cfp.fmap[key]
		if f == nil {
			return nil, errors.WithStack(os.ErrNotExist)
		}

		reader, err := f.Open()
		if err != nil {
			return nil, errors.WithStack(err)
		}
		cfp.readSeeker = newSpoolReader(reader, int64(f.UncompressedSize64), cfp.options)
		cfp.seekFileIndex = fileIndex
	}

	return cfp.readSeeker, nil
}

// Close closes all reader belonging to this ZipPool
func (cfp *ZipPool) Close() error {
	var errs []error
	// On failure the reader is kept so the next Get* call retries the close,
	// but the index is cleared so a closed reader is never handed back.
	if cfp.reader != nil {
		if err := cfp.reader.Close(); err != nil {
			errs = append(errs, err)
		} else {
			cfp.reader = nil
		}
		cfp.fileIndex = -1
	}
	// A streaming close failure must not leave a seekable temp file behind.
	if cfp.readSeeker != nil {
		if err := cfp.readSeeker.Close(); err != nil {
			errs = append(errs, err)
		} else {
			cfp.readSeeker = nil
		}
		cfp.seekFileIndex = -1
	}
	return errors.WithStack(stderrors.Join(errs...))
}
