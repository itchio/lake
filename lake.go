package lake

import (
	"io"
	"os"
)

// A Pool gives read+seek access to an ordered list of files, by index
type Pool interface {
	GetSize(fileIndex int64) int64
	GetReader(fileIndex int64) (io.Reader, error)
	GetReadSeeker(fileIndex int64) (io.ReadSeeker, error)
	Close() error
}

// A WritablePool adds writing access to the Pool type
type WritablePool interface {
	Pool

	GetWriter(fileIndex int64) (io.WriteCloser, error)
}

// A TruncatablePool adds the ability to truncate files
type TruncatablePool interface {
	WritablePool

	Stat(fileIndex int64) (os.FileInfo, error)
	GetWriterAndTruncate(fileIndex int64, size int64) (io.WriteCloser, error)
}
