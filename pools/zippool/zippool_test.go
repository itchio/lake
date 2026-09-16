package zippool

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	arkivezip "github.com/itchio/arkive/zip"
	"github.com/itchio/lake/tlc"
)

const largeSize = 1 << 20

// largeContent is compressible but position-dependent, so head and tail
// reads can be checked byte for byte.
func largeContent() []byte {
	b := make([]byte, largeSize)
	for i := range b {
		b[i] = byte(i>>10) + byte(i%7)
	}
	return b
}

type entry struct {
	name string
	data []byte
}

func fixture(t *testing.T, entries ...entry) (*tlc.Container, *arkivezip.Reader) {
	t.Helper()
	if entries == nil {
		entries = []entry{
			{"first", []byte("first contents")},
			{"second", []byte("second contents")},
			{"large", largeContent()},
		}
	}
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	c := &tlc.Container{}
	for _, e := range entries {
		w, err := writer.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatal(err)
		}
		c.Files = append(c.Files, &tlc.File{Path: e.name, Size: int64(len(e.data))})
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := arkivezip.NewReader(bytes.NewReader(data.Bytes()), int64(data.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return c, reader
}

func tempFiles(t *testing.T, dir string, want int) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != want {
		t.Fatalf("temp files = %d, want %d", len(entries), want)
	}
	return entries
}

func readN(t *testing.T, r io.Reader, n int) []byte {
	t.Helper()
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatal(err)
	}
	return buf
}

func expectBytes(t *testing.T, got, want []byte) {
	t.Helper()
	if !bytes.Equal(got, want) {
		if len(got) < 64 && len(want) < 64 {
			t.Fatalf("read %q, want %q", got, want)
		}
		t.Fatalf("read %d bytes, mismatch with %d wanted bytes", len(got), len(want))
	}
}

func seekTo(t *testing.T, r io.Seeker, off int64, whence int) {
	t.Helper()
	if _, err := r.Seek(off, whence); err != nil {
		t.Fatal(err)
	}
}

func spool(t *testing.T, r io.ReadSeeker) *spoolReader {
	t.Helper()
	s, ok := r.(*spoolReader)
	if !ok {
		t.Fatalf("seek reader is %T, want *spoolReader", r)
	}
	return s
}

func TestHeadReadDoesNotSpill(t *testing.T) {
	c, zr := fixture(t)
	dir := t.TempDir()
	p := NewWithOptions(c, zr, Options{MaxMemory: 4096, TempDir: dir})
	defer p.Close()
	r, err := p.GetReadSeeker(2)
	if err != nil {
		t.Fatal(err)
	}
	want := largeContent()
	expectBytes(t, readN(t, r, 8), want[:8])
	if s := spool(t, r); s.spooled > spoolChunk {
		t.Fatalf("head read spooled %d bytes", s.spooled)
	}
	tempFiles(t, dir, 0)
}

func TestTailReadSpillsToDisk(t *testing.T) {
	c, zr := fixture(t)
	dir := t.TempDir()
	p := NewWithOptions(c, zr, Options{MaxMemory: 4096, TempDir: dir})
	defer p.Close()
	r, err := p.GetReadSeeker(2)
	if err != nil {
		t.Fatal(err)
	}
	want := largeContent()
	seekTo(t, r, -8, io.SeekEnd)
	expectBytes(t, readN(t, r, 8), want[largeSize-8:])
	tempFiles(t, dir, 1)
	if s := spool(t, r); s.mem != nil {
		t.Fatal("memory spool kept after spill")
	}
	seekTo(t, r, 0, io.SeekStart)
	expectBytes(t, readN(t, r, 8), want[:8])
	seekTo(t, r, 1000, io.SeekStart)
	all, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	expectBytes(t, all, want[1000:])
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	tempFiles(t, dir, 0)
}

func TestSmallEntryStaysInMemory(t *testing.T) {
	c, zr := fixture(t)
	dir := t.TempDir()
	p := NewWithOptions(c, zr, Options{TempDir: dir})
	defer p.Close()
	r, err := p.GetReadSeeker(0)
	if err != nil {
		t.Fatal(err)
	}
	all, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	expectBytes(t, all, []byte("first contents"))
	seekTo(t, r, -8, io.SeekEnd)
	expectBytes(t, readN(t, r, 8), []byte("contents"))
	tempFiles(t, dir, 0)
}

func TestSpillMidRead(t *testing.T) {
	c, zr := fixture(t)
	dir := t.TempDir()
	p := NewWithOptions(c, zr, Options{MaxMemory: 4096, TempDir: dir})
	defer p.Close()
	r, err := p.GetReadSeeker(2)
	if err != nil {
		t.Fatal(err)
	}
	want := largeContent()
	expectBytes(t, readN(t, r, 8), want[:8])
	tempFiles(t, dir, 0)
	rest, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	expectBytes(t, rest, want[8:])
	tempFiles(t, dir, 1)
	seekTo(t, r, 4090, io.SeekStart)
	// straddles the memory bytes that moved to disk and the bytes written after
	expectBytes(t, readN(t, r, 12), want[4090:4102])
}

func TestRepeatedGetReaderStartsOver(t *testing.T) {
	c, zr := fixture(t)
	p := New(c, zr)
	defer p.Close()
	first, err := p.GetReader(0)
	if err != nil {
		t.Fatal(err)
	}
	expectBytes(t, readN(t, first, 5), []byte("first"))
	again, err := p.GetReader(0)
	if err != nil {
		t.Fatal(err)
	}
	all, err := io.ReadAll(again)
	if err != nil {
		t.Fatal(err)
	}
	expectBytes(t, all, []byte("first contents"))
	if _, err := first.Read(make([]byte, 1)); err == nil {
		t.Fatal("previous reader left open")
	}
}

func TestSwitchingEntryDropsSpool(t *testing.T) {
	c, zr := fixture(t)
	dir := t.TempDir()
	p := NewWithOptions(c, zr, Options{MaxMemory: 4096, TempDir: dir})
	defer p.Close()
	r, err := p.GetReadSeeker(2)
	if err != nil {
		t.Fatal(err)
	}
	seekTo(t, r, -1, io.SeekEnd)
	readN(t, r, 1)
	tempFiles(t, dir, 1)

	// streaming reads share nothing with the seekable entry
	stream, err := p.GetReader(0)
	if err != nil {
		t.Fatal(err)
	}
	all, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	expectBytes(t, all, []byte("first contents"))
	again, err := p.GetReadSeeker(2)
	if err != nil {
		t.Fatal(err)
	}
	if again != r {
		t.Fatal("same entry lost its cached reader")
	}

	next, err := p.GetReadSeeker(0)
	if err != nil {
		t.Fatal(err)
	}
	expectBytes(t, readN(t, next, 5), []byte("first"))
	tempFiles(t, dir, 0)
	if _, err := r.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("old reader not closed: %v", err)
	}
	if _, err := r.Seek(0, io.SeekStart); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("old reader seekable after close: %v", err)
	}
}

func TestCloseIsIdempotentAndReopens(t *testing.T) {
	c, zr := fixture(t)
	dir := t.TempDir()
	p := NewWithOptions(c, zr, Options{MaxMemory: 4096, TempDir: dir})
	r, err := p.GetReadSeeker(2)
	if err != nil {
		t.Fatal(err)
	}
	seekTo(t, r, -1, io.SeekEnd)
	readN(t, r, 1)
	tempFiles(t, dir, 1)
	for range 2 {
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
	}
	tempFiles(t, dir, 0)
	r, err = p.GetReadSeeker(0)
	if err != nil {
		t.Fatal(err)
	}
	expectBytes(t, readN(t, r, 5), []byte("first"))
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestChecksumFailureSurfacesOnRead(t *testing.T) {
	c, zr := fixture(t)
	zr.File[2].CRC32 ^= 1
	dir := t.TempDir()
	p := NewWithOptions(c, zr, Options{MaxMemory: 4096, TempDir: dir})
	defer p.Close()
	r, err := p.GetReadSeeker(2)
	if err != nil {
		t.Fatal(err)
	}
	// the head is fine; the checksum is only verified at the end
	expectBytes(t, readN(t, r, 8), largeContent()[:8])
	seekTo(t, r, -8, io.SeekEnd)
	if _, err := r.Read(make([]byte, 8)); err == nil {
		t.Fatal("expected checksum failure")
	}
	// a bad checksum taints everything already handed out, so stay failed
	seekTo(t, r, 0, io.SeekStart)
	if _, err := r.Read(make([]byte, 8)); err == nil {
		t.Fatal("error not sticky")
	}
	next, err := p.GetReadSeeker(0)
	if err != nil {
		t.Fatal(err)
	}
	expectBytes(t, readN(t, next, 5), []byte("first"))
	tempFiles(t, dir, 0)
}

func TestMissingTempDir(t *testing.T) {
	c, zr := fixture(t)
	dir := filepath.Join(t.TempDir(), "missing")
	p := NewWithOptions(c, zr, Options{MaxMemory: 4096, TempDir: dir})
	defer p.Close()
	r, err := p.GetReadSeeker(2)
	if err != nil {
		t.Fatal(err)
	}
	// reads within the memory cap never need the directory
	expectBytes(t, readN(t, r, 8), largeContent()[:8])
	seekTo(t, r, -8, io.SeekEnd)
	if _, err := r.Read(make([]byte, 8)); err == nil {
		t.Fatal("expected spill failure")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = p.GetReadSeeker(2)
	if err != nil {
		t.Fatal(err)
	}
	seekTo(t, r, -8, io.SeekEnd)
	expectBytes(t, readN(t, r, 8), largeContent()[largeSize-8:])
}

func TestSeekSemantics(t *testing.T) {
	c, zr := fixture(t)
	p := NewWithOptions(c, zr, Options{TempDir: t.TempDir()})
	defer p.Close()
	r, err := p.GetReadSeeker(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Seek(-1, io.SeekStart); err == nil {
		t.Fatal("negative seek accepted")
	}
	if _, err := r.Seek(0, 7); err == nil {
		t.Fatal("bad whence accepted")
	}
	pos, err := r.Seek(0, io.SeekEnd)
	if err != nil || pos != int64(len("first contents")) {
		t.Fatalf("SeekEnd = %d, %v", pos, err)
	}
	if n, err := r.Read(make([]byte, 4)); n != 0 || err != io.EOF {
		t.Fatalf("read at end = %d, %v", n, err)
	}
	seekTo(t, r, 100, io.SeekStart)
	if n, err := r.Read(make([]byte, 4)); n != 0 || err != io.EOF {
		t.Fatalf("read past end = %d, %v", n, err)
	}
	seekTo(t, r, 6, io.SeekStart)
	seekTo(t, r, -6, io.SeekCurrent)
	expectBytes(t, readN(t, r, 5), []byte("first"))
	if n, err := r.Read(nil); n != 0 || err != nil {
		t.Fatalf("empty read = %d, %v", n, err)
	}
}

func TestDeclaredSizeShorterThanStream(t *testing.T) {
	r := newSpoolReader(io.NopCloser(strings.NewReader("0123456789")), 4, Options{TempDir: t.TempDir()})
	defer r.Close()
	seekTo(t, r, -2, io.SeekEnd)
	// the header wins for positioning, the stream for content
	expectBytes(t, readN(t, r, 2), []byte("23"))
	rest, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	expectBytes(t, rest, []byte("456789"))
}

func TestDeclaredSizeLongerThanStream(t *testing.T) {
	r := newSpoolReader(io.NopCloser(strings.NewReader("0123")), 10, Options{TempDir: t.TempDir()})
	defer r.Close()
	seekTo(t, r, -2, io.SeekEnd)
	if n, err := r.Read(make([]byte, 2)); n != 0 || err != io.EOF {
		t.Fatalf("read beyond stream = %d, %v", n, err)
	}
	seekTo(t, r, 2, io.SeekStart)
	all, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	expectBytes(t, all, []byte("23"))
}

type flakyCloser struct {
	ReadCloseSeeker
	failures int
	closes   int
}

func (f *flakyCloser) Close() error {
	f.closes++
	if f.closes <= f.failures {
		return errors.New("close failed")
	}
	return f.ReadCloseSeeker.Close()
}

func TestSeekReopensAfterCloseFailure(t *testing.T) {
	c, zr := fixture(t)
	dir := t.TempDir()
	p := NewWithOptions(c, zr, Options{MaxMemory: 4096, TempDir: dir})
	defer p.Close()
	r, err := p.GetReadSeeker(2)
	if err != nil {
		t.Fatal(err)
	}
	seekTo(t, r, -1, io.SeekEnd)
	readN(t, r, 1)
	flaky := &flakyCloser{ReadCloseSeeker: p.readSeeker, failures: 1}
	p.readSeeker = flaky
	if err := p.Close(); err == nil {
		t.Fatal("expected close failure")
	}
	// The failed close must not be mistaken for a live cached entry.
	r, err = p.GetReadSeeker(2)
	if err != nil {
		t.Fatal(err)
	}
	if r == ReadCloseSeeker(flaky) {
		t.Fatal("closed reader returned from cache")
	}
	if flaky.closes != 2 {
		t.Fatalf("close retried %d times, want 1", flaky.closes-1)
	}
	expectBytes(t, readN(t, r, 8), largeContent()[:8])
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	tempFiles(t, dir, 0)
}

type closeFailure struct{}

func (closeFailure) Read([]byte) (int, error) { return 0, io.EOF }
func (closeFailure) Close() error             { return errors.New("stream close failed") }

func TestSpoolCleanupAfterStreamCloseFailure(t *testing.T) {
	c, zr := fixture(t)
	dir := t.TempDir()
	p := NewWithOptions(c, zr, Options{MaxMemory: 4096, TempDir: dir})
	r, err := p.GetReadSeeker(2)
	if err != nil {
		t.Fatal(err)
	}
	seekTo(t, r, -1, io.SeekEnd)
	readN(t, r, 1)
	tempFiles(t, dir, 1)
	p.reader = closeFailure{}
	if err := p.Close(); err == nil {
		t.Fatal("expected stream close failure")
	}
	tempFiles(t, dir, 0)
}

func TestSpoolRetriesRemoval(t *testing.T) {
	dir := t.TempDir()
	r := newSpoolReader(io.NopCloser(strings.NewReader("contents")), 8, Options{MaxMemory: 1, TempDir: dir})
	if _, err := io.ReadAll(r); err != nil {
		t.Fatal(err)
	}
	name := r.file.Name()
	// Swap the temp file for a nonempty directory so removal fails portably.
	if err := r.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(name, 0700); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(name, "child")
	if err := os.WriteFile(child, nil, 0600); err != nil {
		t.Fatal(err)
	}
	r.closed = true
	if err := r.Close(); err == nil {
		t.Fatal("expected removal failure")
	}
	if err := os.Remove(child); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	tempFiles(t, dir, 0)
}

type boundedZeroReader struct{}

func (boundedZeroReader) Read(p []byte) (int, error) {
	if len(p) > spoolChunk {
		return 0, errors.New("unbounded read buffer")
	}
	clear(p)
	return len(p), nil
}

func TestSpoolStreamsLargeEntry(t *testing.T) {
	dir := t.TempDir()
	const size = 16 << 20
	src := io.NopCloser(io.LimitReader(boundedZeroReader{}, size))
	r := newSpoolReader(src, size, Options{MaxMemory: 1 << 20, TempDir: dir})
	defer r.Close()
	seekTo(t, r, -1, io.SeekEnd)
	readN(t, r, 1)
	st, err := r.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != size {
		t.Fatalf("spooled %d, want %d", st.Size(), size)
	}
}

func BenchmarkZipSeek(b *testing.B) {
	const size = 16 << 20
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	w, err := zw.Create("large")
	if err != nil {
		b.Fatal(err)
	}
	if _, err := io.Copy(w, io.LimitReader(boundedZeroReader{}, size)); err != nil {
		b.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		b.Fatal(err)
	}
	zr, err := arkivezip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		b.Fatal(err)
	}
	c := &tlc.Container{Files: []*tlc.File{{Path: "large", Size: size}}}
	for _, tail := range []bool{false, true} {
		name := "head"
		if tail {
			name = "tail"
		}
		b.Run(name, func(b *testing.B) {
			p := NewWithOptions(c, zr, Options{MaxMemory: 1 << 20, TempDir: b.TempDir()})
			defer p.Close()
			buf := make([]byte, 8)
			b.ReportAllocs()
			b.SetBytes(size)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r, err := p.GetReadSeeker(0)
				if err != nil {
					b.Fatal(err)
				}
				if tail {
					if _, err := r.Seek(-8, io.SeekEnd); err != nil {
						b.Fatal(err)
					}
				}
				if _, err := io.ReadFull(r, buf); err != nil {
					b.Fatal(err)
				}
				if err := p.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

type recordingCloser struct {
	log      *[]string
	name     string
	failures int
}

func (c *recordingCloser) Close() error {
	if c.failures > 0 {
		c.failures--
		return errors.New(c.name + " close failed")
	}
	*c.log = append(*c.log, c.name)
	return nil
}

type recordingReader struct {
	io.Reader
	*recordingCloser
}

func TestOwnedArchiveClosedLastAndOnce(t *testing.T) {
	c, zr := fixture(t)
	p := NewWithOptions(c, zr, Options{TempDir: t.TempDir()})
	var log []string
	p.OwnArchive(&recordingCloser{log: &log, name: "archive"})
	if _, err := p.GetReadSeeker(0); err != nil {
		t.Fatal(err)
	}
	p.reader = recordingReader{strings.NewReader(""), &recordingCloser{log: &log, name: "entry"}}
	for range 2 {
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(log, ","); got != "entry,archive" {
		t.Fatalf("close order %q", got)
	}
}

func TestOwnedArchiveCloseRetried(t *testing.T) {
	c, zr := fixture(t)
	p := NewWithOptions(c, zr, Options{TempDir: t.TempDir()})
	var log []string
	p.OwnArchive(&recordingCloser{log: &log, name: "archive", failures: 1})
	if err := p.Close(); err == nil {
		t.Fatal("expected archive close failure")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 {
		t.Fatalf("archive closed %d times", len(log))
	}
}

func TestCallerOwnedArchiveLeftOpen(t *testing.T) {
	c, zr := fixture(t)
	p := New(c, zr)
	if _, err := p.GetReadSeeker(0); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	// the archive is still open, so the pool can keep serving entries
	r, err := p.GetReadSeeker(1)
	if err != nil {
		t.Fatal(err)
	}
	expectBytes(t, readN(t, r, 6), []byte("second"))
}
