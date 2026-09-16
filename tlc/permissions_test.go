package tlc

import (
	"bytes"
	"io"
	"testing"
)

type fakePool struct {
	files  [][]byte
	closed bool
}

func (p *fakePool) GetSize(i int64) int64 { return int64(len(p.files[i])) }
func (p *fakePool) GetReader(i int64) (io.Reader, error) {
	return bytes.NewReader(p.files[i]), nil
}
func (p *fakePool) GetReadSeeker(i int64) (io.ReadSeeker, error) {
	return bytes.NewReader(p.files[i]), nil
}
func (p *fakePool) Close() error {
	p.closed = true
	return nil
}

func Test_FixPermissions(t *testing.T) {
	pool := &fakePool{files: [][]byte{
		[]byte("\x7fELF rest of binary"),
		[]byte("plain text"),
		[]byte("#!"),
	}}
	c := &Container{}
	for _, f := range pool.files {
		c.Files = append(c.Files, &File{Mode: 0o644, Size: int64(len(f))})
	}
	if err := c.FixPermissions(pool); err != nil {
		t.Fatal(err)
	}
	if c.Files[0].Mode != 0o755 {
		t.Fatalf("ELF mode = %o", c.Files[0].Mode)
	}
	if c.Files[1].Mode != 0o644 {
		t.Fatalf("text mode = %o", c.Files[1].Mode)
	}
	// too short to scan; left alone
	if c.Files[2].Mode != 0o644 {
		t.Fatalf("short file mode = %o", c.Files[2].Mode)
	}
	if pool.closed {
		t.Fatal("FixPermissions closed a pool it does not own")
	}
}
