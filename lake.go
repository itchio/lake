package lake

import "io"

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
