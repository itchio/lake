package lake

import (
	"io"
	"strings"

	"github.com/itchio/headway/state"
)

// A Pool gives read+seek access to an ordered list of files, by index
type Pool interface {
	// GetSize returns the size of a given file entry, as specified by the container
	// the pool was built with.
	GetSize(fileIndex int64) int64

	// GetReader returns a Reader for a given file. Typically, readers are cached,
	// so a second call to GetReader will close the last reader.
	GetReader(fileIndex int64) (io.Reader, error)

	// GetReadSeeker beahves like GetReader (including caching) but allows seeking
	// as well. For some pools (like zip pool), this call may involve decompressing
	// *an entire entry* and then returning a temporary *os.File (or memory file).
	GetReadSeeker(fileIndex int64) (io.ReadSeeker, error)

	// Close closes the last opened reader, if any. Does not impact `GetWriter`
	// at all. Calling Close doesn't render the pool unusable, all its other methods
	// should not error out afterwards.
	Close() error
}

// A WritablePool adds writing access to the Pool type
type WritablePool interface {
	Pool

	// GetWriter returns a writer for a given file entry
	// This also truncates the file on disk (or whatever the pool represents),
	// so that the file's final size is the number of bytes written on close.
	// Writers aren't cached, so this can be called concurrently.
	GetWriter(fileIndex int64) (io.WriteCloser, error)
}

type CaseFix struct {
	// Case we found on disk, which was wrong
	Old string
	// Case we renamed it to, which is right
	New string
}

func (cf CaseFix) Apply(entryPath string) (string, bool) {
	if entryPath == cf.Old {
		return cf.New, true
	}

	oldPrefix := cf.Old + "/"
	if strings.HasPrefix(entryPath, oldPrefix) {
		newPrefix := cf.New + "/"
		return strings.Replace(entryPath, oldPrefix, newPrefix, 1), true
	}

	return entryPath, false
}

type CaseFixStats struct {
	Fixes []CaseFix
}

type CaseFixParams struct {
	Stats    *CaseFixStats
	Consumer *state.Consumer
}

type CaseFixerPool interface {
	// FixExistingCase is ugly, but so is the real world.
	//
	// It collects all the paths in the pool's container, and
	// from shortest to longest, makes sure that they have the
	// case we expected.
	//
	// For example, if we have container with files:
	//   - Foo/Bar
	// And directories:
	//   - Foo/
	// But on disk, we have:
	//   - FOO/bar
	//
	// This would rename `FOO` to `Foo`,
	// and `Foo/bar` to `Foo/Bar`.
	FixExistingCase(params CaseFixParams) error
}
