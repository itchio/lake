package fspool

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/itchio/lake"
	"github.com/itchio/lake/tlc"
	"github.com/itchio/screw"
	"github.com/pkg/errors"
)

const (
	// ModeMask is or'd with the permission files being opened
	ModeMask = 0o644
)

type fsEntryReader interface {
	io.ReadSeeker
	io.Closer
}

// FsPool is a filesystem-backed Pool+WritablePool
type FsPool struct {
	container *tlc.Container
	basePath  string

	fileIndex int64
	reader    fsEntryReader

	UniqueReader fsEntryReader
}

var _ lake.Pool = (*FsPool)(nil)
var _ lake.WritablePool = (*FsPool)(nil)
var _ lake.CaseFixerPool = (*FsPool)(nil)

// ReadCloseSeeker unifies io.Reader, io.Seeker, and io.Closer
type ReadCloseSeeker interface {
	io.Reader
	io.Seeker
	io.Closer
}

// New creates a new FsPool from the given Container
// metadata and a base path on-disk to allow reading from files.
func New(c *tlc.Container, basePath string) *FsPool {
	return &FsPool{
		container: c,
		basePath:  basePath,

		fileIndex: int64(-1),
		reader:    nil,
	}
}

// GetSize returns the size of the file at index fileIndex
func (cfp *FsPool) GetSize(fileIndex int64) int64 {
	return cfp.container.Files[fileIndex].Size
}

// GetRelativePath returns the slashed path of a file, relative to
// the container's root.
func (cfp *FsPool) GetRelativePath(fileIndex int64) string {
	return cfp.container.Files[fileIndex].Path
}

// GetPath returns the native path of a file (with slashes or backslashes)
// on-disk, based on the FsPool's base path
func (cfp *FsPool) GetPath(fileIndex int64) string {
	return cfp.diskPath(cfp.GetRelativePath(fileIndex))
}

func (cfp *FsPool) diskPath(containerPath string) string {
	return filepath.Join(cfp.basePath, filepath.FromSlash(containerPath))
}

// GetReader returns an io.Reader for the file at index fileIndex
// Successive calls to `GetReader` will attempt to re-use the last
// returned reader if the file index is similar. The cache size is 1, so
// reading in parallel from different files is not supported.
func (cfp *FsPool) GetReader(fileIndex int64) (io.Reader, error) {
	rs, err := cfp.GetReadSeeker(fileIndex)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	_, err = rs.Seek(0, io.SeekStart)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return rs, nil
}

// GetReadSeeker is like GetReader but the returned object allows seeking
func (cfp *FsPool) GetReadSeeker(fileIndex int64) (io.ReadSeeker, error) {
	if cfp.UniqueReader != nil {
		return cfp.UniqueReader, nil
	}

	if cfp.fileIndex != fileIndex {
		if cfp.reader != nil {
			err := cfp.reader.Close()
			if err != nil {
				return nil, errors.WithStack(err)
			}
			cfp.reader = nil
		}

		reader, err := screw.Open(cfp.GetPath(fileIndex))
		if err != nil {
			return nil, err
		}

		cfp.reader = reader
		cfp.fileIndex = fileIndex
	}

	return cfp.reader, nil
}

// Close closes all reader belonging to this FsPool
func (cfp *FsPool) Close() error {
	if cfp.reader != nil {
		err := cfp.reader.Close()
		if err != nil {
			return errors.WithStack(err)
		}

		cfp.reader = nil
		cfp.fileIndex = -1
	}

	return nil
}

// GetWriter returns a writer for one of the container's file.
// It creates the file if it doesn't exist, and always truncates it.
func (cfp *FsPool) GetWriter(fileIndex int64) (io.WriteCloser, error) {
	path := cfp.GetPath(fileIndex)

	err := screw.MkdirAll(filepath.Dir(path), os.FileMode(0o755))
	if err != nil {
		return nil, errors.WithStack(err)
	}

	stats, err := screw.Lstat(path)
	if err == nil {
		// did stat
		if stats.IsDir() {
			err := screw.RemoveAll(path)
			if err != nil {
				return nil, errors.WithStack(err)
			}
		} else if stats.Mode()&os.ModeSymlink > 0 {
			err := screw.Remove(path)
			if err != nil {
				return nil, errors.WithStack(err)
			}
		}
	}

	outputFile := cfp.container.Files[fileIndex]
	f, oErr := screw.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(outputFile.Mode)|ModeMask)
	if oErr != nil {
		return nil, oErr
	}

	return f, nil
}

func (cfp *FsPool) FixExistingCase(params lake.CaseFixParams) error {
	if !screw.IsCaseInsensitiveFS() {
		return nil
	}

	var paths []string

	cfp.container.ForEachEntry(func(e tlc.Entry) tlc.ForEachOutcome {
		paths = append(paths, e.GetPath())
		return tlc.ForEachContinue
	})

	sort.Slice(paths, func(i, j int) bool {
		return len(paths[i]) < len(paths[j])
	})

	for len(paths) > 0 {
		p := paths[0]
		paths = paths[1:]

		needBaseName := path.Base(p)
		haveBaseName := screw.TrueBaseName(cfp.diskPath(p))
		if haveBaseName != "" && haveBaseName != needBaseName {
			// important: don't use filepath.Join() here, use path.Join()
			havePath := path.Join(path.Dir(p), haveBaseName)
			needPath := p

			if params.Consumer != nil {
				params.Consumer.Debugf("Fixing: (%s) => (%s)", havePath, needPath)
			}

			err := screw.Rename(cfp.diskPath(havePath), cfp.diskPath(needPath))
			if err != nil {
				return errors.WithStack(err)
			}

			fix := lake.CaseFix{
				Old: havePath,
				New: needPath,
			}
			if params.Stats != nil {
				params.Stats.Fixes = append(params.Stats.Fixes, fix)
			}

			for i, p := range paths {
				if newP, changed := fix.Apply(p); changed {
					params.Consumer.Debugf("Hence, (%s) => (%s)", p, newP)
					paths[i] = newP
				}
			}
		}
	}

	return nil
}
