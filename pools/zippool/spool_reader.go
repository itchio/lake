package zippool

import (
	"errors"
	"io"
	"os"
)

// DefaultMaxMemory applies when Options.MaxMemory is zero.
const DefaultMaxMemory int64 = 32 << 20

const spoolChunk = 64 << 10

// spoolReader backs ZipPool.GetReadSeeker. Zip entries only stream, so it
// decompresses on demand and keeps what it has produced so far in a spool,
// first in memory and past MaxMemory in a temp file.
type spoolReader struct {
	src     io.ReadCloser
	srcErr  error
	size    int64
	pos     int64
	spooled int64
	mem     []byte
	file    *os.File
	limit   int64
	tempDir string
	buf     []byte
	closed  bool
}

func newSpoolReader(src io.ReadCloser, size int64, opts Options) *spoolReader {
	limit := opts.MaxMemory
	if limit <= 0 {
		limit = DefaultMaxMemory
	}
	return &spoolReader{src: src, size: size, limit: limit, tempDir: opts.TempDir}
}

func (r *spoolReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, os.ErrClosed
	}
	if r.srcErr != nil {
		return 0, r.srcErr
	}
	if len(p) == 0 {
		return 0, nil
	}
	if err := r.fill(r.pos + int64(len(p))); err != nil {
		return 0, err
	}
	if r.pos >= r.spooled {
		return 0, io.EOF
	}
	if avail := r.spooled - r.pos; int64(len(p)) > avail {
		p = p[:avail]
	}
	var n int
	if r.file != nil {
		var err error
		n, err = r.file.ReadAt(p, r.pos)
		if err != nil && err != io.EOF {
			return n, err
		}
	} else {
		n = copy(p, r.mem[r.pos:])
	}
	r.pos += int64(n)
	return n, nil
}

// fill spools through target. Reaching the declared size drains the source
// so the decompressor's checksum check runs. Failures are sticky: a chunk
// that could not be spooled is gone, and a bad checksum taints earlier bytes.
func (r *spoolReader) fill(target int64) error {
	for r.src != nil && (r.spooled < target || r.spooled >= r.size) {
		if r.file == nil && r.spooled >= r.limit {
			if err := r.spill(); err != nil {
				return r.fail(err)
			}
		}
		if r.buf == nil {
			r.buf = make([]byte, spoolChunk)
		}
		buf := r.buf
		if r.file == nil && r.limit-r.spooled < int64(len(buf)) {
			buf = buf[:r.limit-r.spooled]
		}
		n, err := r.src.Read(buf)
		if n > 0 {
			if werr := r.spool(buf[:n]); werr != nil {
				return r.fail(werr)
			}
		}
		if err == io.EOF {
			cerr := r.src.Close()
			r.src = nil
			if cerr != nil {
				return r.fail(cerr)
			}
			return nil
		}
		if err != nil {
			return r.fail(err)
		}
	}
	return nil
}

func (r *spoolReader) fail(err error) error {
	r.srcErr = err
	if r.src != nil {
		r.src.Close()
		r.src = nil
	}
	return err
}

func (r *spoolReader) spill() error {
	f, err := os.CreateTemp(r.tempDir, "lake-zip-*")
	if err != nil {
		return err
	}
	if _, err := f.WriteAt(r.mem, 0); err != nil {
		return errors.Join(err, f.Close(), os.Remove(f.Name()))
	}
	r.file = f
	r.mem = nil
	return nil
}

func (r *spoolReader) spool(chunk []byte) error {
	if r.file != nil {
		if _, err := r.file.WriteAt(chunk, r.spooled); err != nil {
			return err
		}
	} else {
		r.mem = append(r.mem, chunk...)
	}
	r.spooled += int64(len(chunk))
	return nil
}

// Seek resolves SeekEnd against the header's declared size so it costs
// nothing until a read follows. Positions past the end are allowed, as with
// a file.
func (r *spoolReader) Seek(offset int64, whence int) (int64, error) {
	if r.closed {
		return 0, os.ErrClosed
	}
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = r.pos + offset
	case io.SeekEnd:
		abs = r.size + offset
	default:
		return 0, errors.New("zippool: invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("zippool: negative seek position")
	}
	r.pos = abs
	return abs, nil
}

// Close may be called more than once. The temp file is closed before removal
// because Windows refuses to remove an open file; if removal fails, a later
// Close retries it without touching the descriptor again.
func (r *spoolReader) Close() error {
	var errs []error
	if !r.closed {
		r.closed = true
		r.mem = nil
		if r.src != nil {
			errs = append(errs, r.src.Close())
			r.src = nil
		}
		if r.file != nil {
			errs = append(errs, r.file.Close())
		}
	}
	if r.file != nil {
		err := os.Remove(r.file.Name())
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		if err == nil {
			r.file = nil
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
