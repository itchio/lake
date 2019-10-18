package fspool_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/itchio/headway/state"
	"github.com/itchio/lake"
	"github.com/itchio/lake/pools/fspool"
	"github.com/itchio/lake/tlc"
	"github.com/itchio/screw"
	"github.com/stretchr/testify/assert"
)

func Test_FixExistingCase(t *testing.T) {
	if !screw.IsCaseInsensitiveFS() {
		t.Skip("FixExistingCase is only relevant on case-insensitive filesystems")
	}

	assert := assert.New(t)

	tempDir, err := ioutil.TempDir("", "")
	must(t, err)

	v1 := filepath.Join(tempDir, "v1")
	err = os.MkdirAll(filepath.Join(v1, "foo", "bar"), 0o755)
	must(t, err)
	err = ioutil.WriteFile(filepath.Join(v1, "foo", "bar", "baz"), []byte("ahHA"), 0o644)
	must(t, err)

	v2 := filepath.Join(tempDir, "v2")
	err = os.MkdirAll(filepath.Join(v2, "FOO", "BAR"), 0o755)
	must(t, err)
	err = ioutil.WriteFile(filepath.Join(v2, "FOO", "BAR", "BAZ"), []byte("ahHA"), 0o644)
	must(t, err)

	container, err := tlc.WalkAny(v2, &tlc.WalkOpts{})
	must(t, err)

	fsp := fspool.New(container, v1)
	stats := lake.CaseFixStats{}
	consumer := &state.Consumer{
		OnMessage: func(lvl string, msg string) {
			t.Logf("[%s] %s", lvl, msg)
		},
	}

	err = fsp.FixExistingCase(lake.CaseFixParams{
		Stats:    &stats,
		Consumer: consumer,
	})
	must(t, err)

	assert.EqualValues(3, len(stats.Fixes), "should have done 3 renames")
	if len(stats.Fixes) == 3 {
		assert.EqualValues(lake.CaseFix{
			Old: "foo",
			New: "FOO",
		}, stats.Fixes[0])
		assert.EqualValues(lake.CaseFix{
			Old: "FOO/bar",
			New: "FOO/BAR",
		}, stats.Fixes[1])
		assert.EqualValues(lake.CaseFix{
			Old: "FOO/BAR/baz",
			New: "FOO/BAR/BAZ",
		}, stats.Fixes[2])
	}
}

func must(t *testing.T, err error) {
	if err != nil {
		assert.NoError(t, err)
		t.FailNow()
	}
}
