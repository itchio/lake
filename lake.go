package lake

import (
	"io"
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
