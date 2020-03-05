package pools_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/itchio/lake"
	"github.com/itchio/lake/pools/cachepool"
	"github.com/itchio/lake/pools/fspool"
	"github.com/itchio/lake/tlc"
	"github.com/stretchr/testify/assert"
)

// Close must leave pools in a usable state
func Test_Close(t *testing.T) {
	assert := assert.New(t)

	tmpPath, err := ioutil.TempDir("", "tmp_test_close")
	must(t, err)

	defer os.RemoveAll(tmpPath)

	contents := []byte("Hello!")
	err = ioutil.WriteFile(filepath.Join(tmpPath, "hello.txt"), contents, os.FileMode(0o644))
	must(t, err)

	container, err := tlc.WalkDir(tmpPath, tlc.WalkOpts{})
	must(t, err)

	testPool := func(p lake.Pool) {
		for i := 0; i < 2; i++ {
			r, err := p.GetReader(0)
			must(t, err)

			readBytes, err := ioutil.ReadAll(r)
			must(t, err)

			assert.EqualValues(contents, readBytes)

			err = p.Close()
			must(t, err)
		}
	}

	fp := fspool.New(container, tmpPath)
	testPool(fp)

	cachePath, err := ioutil.TempDir("", "tmp_test_cache")
	must(t, err)

	cacheTarget := fspool.New(container, cachePath)

	cp := cachepool.New(container, fp, cacheTarget)
	go func() {
		for i := range container.Files {
			_ = cp.Preload(int64(i))
		}
	}()

	testPool(cp)
}

func must(t *testing.T, err error) {
	if err != nil {
		t.Error("must failed: ", err.Error())
		t.FailNow()
	}
}
