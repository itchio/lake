package cachepool

import (
	"io"
	"sync"

	"github.com/itchio/lake"
	"github.com/itchio/lake/tlc"
	"github.com/pkg/errors"
)

type CachePool struct {
	container *tlc.Container
	source    lake.Pool
	cache     lake.WritablePool

	fileChans     []chan struct{}
	shutdownErr   error
	shutdownMutex sync.Mutex
}

var _ lake.Pool = (*CachePool)(nil)

// New creates a cachepool that reads from source and stores in
// cache as an intermediary
func New(c *tlc.Container, source lake.Pool, cache lake.WritablePool) *CachePool {
	cp := &CachePool{
		container: c,
		source:    source,
		cache:     cache,
		fileChans: make([]chan struct{}, len(c.Files)),
	}

	for i := range cp.fileChans {
		cp.fileChans[i] = make(chan struct{})
	}

	return cp
}

// Preload immediately starts copying from source to cache.
// if it returns nil, all future GetRead{Seek,}er calls for
// this index will succeed (and all pending calls will unblock)
func (cp *CachePool) Preload(fileIndex int64) error {
	err := cp.doPreload(fileIndex)
	if err != nil {
		cp.shutdown(err)
		return err
	}

	return nil
}

func (cp *CachePool) shutdown(err error) {
	cp.shutdownMutex.Lock()
	defer cp.shutdownMutex.Unlock()

	if cp.shutdownErr != nil {
		return
	}
	cp.shutdownErr = err

	for _, channel := range cp.fileChans {
		select {
		case <-channel:
			// already preloaded
		default:
			// close
			close(channel)
		}
	}
}

func (cp *CachePool) doPreload(fileIndex int64) error {
	channel := cp.fileChans[int(fileIndex)]

	select {
	case <-channel:
		// already preloaded, all done!
		return nil
	default:
		// need to preload now
	}

	success := false
	defer func() {
		if success {
			close(channel)
		}
	}()

	reader, err := cp.source.GetReader(fileIndex)
	if err != nil {
		return errors.WithStack(err)
	}
	defer cp.source.Close()

	writer, err := cp.cache.GetWriter(fileIndex)
	if err != nil {
		return errors.WithStack(err)
	}
	defer writer.Close()

	_, err = io.Copy(writer, reader)
	if err != nil {
		return errors.WithStack(err)
	}

	success = true

	return nil
}

func (cp *CachePool) waitFor(fileIndex int64) error {
	// this will block until the channel is closed,
	// or immediately succeed if it's already closed
	<-cp.fileChans[fileIndex]

	cp.shutdownMutex.Lock()
	defer cp.shutdownMutex.Unlock()
	if cp.shutdownErr != nil {
		return errors.WithMessage(cp.shutdownErr, "cache pool was shut down")
	}

	return nil
}

// GetReader returns a reader for the file at index fileIndex,
// once the file has been preloaded successfully.
func (cp *CachePool) GetReader(fileIndex int64) (io.Reader, error) {
	err := cp.waitFor(fileIndex)
	if err != nil {
		return nil, err
	}

	return cp.cache.GetReader(fileIndex)
}

// GetReadSeeker is a version of GetReader that returns an io.ReadSeeker
func (cp *CachePool) GetReadSeeker(fileIndex int64) (io.ReadSeeker, error) {
	err := cp.waitFor(fileIndex)
	if err != nil {
		return nil, err
	}

	return cp.cache.GetReadSeeker(fileIndex)
}

func (cp *CachePool) GetSize(fileIndex int64) int64 {
	return cp.container.Files[fileIndex].Size
}

// Close attempts to close both the source and the cache
// and relays any error it encounters
func (cp *CachePool) Close() error {
	cp.shutdown(errors.New("closed"))

	err := cp.source.Close()
	if err != nil {
		return errors.WithStack(err)
	}

	err = cp.cache.Close()
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}
