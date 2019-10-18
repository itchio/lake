package tlc_test

import (
	"testing"

	"github.com/itchio/lake/tlc"
	"github.com/stretchr/testify/assert"
)

func Test_AssertCaseInsensitiveSafe(t *testing.T) {
	assert := assert.New(t)

	c := &tlc.Container{
		Files: []*tlc.File{
			&tlc.File{
				Path: "foo/bar",
			},
			&tlc.File{
				Path: "FOO/bar",
			},
		},
	}
	assert.Error(c.AssertCaseInsensitiveSafe())

	c = &tlc.Container{
		Files: []*tlc.File{
			&tlc.File{
				Path: "foo/bar",
			},
			&tlc.File{
				Path: "foo/BAR",
			},
		},
	}
	assert.Error(c.AssertCaseInsensitiveSafe())

	c = &tlc.Container{
		Files: []*tlc.File{
			&tlc.File{
				Path: "foo/bar",
			},
			&tlc.File{
				Path: "foo/baz",
			},
		},
	}
	assert.NoError(c.AssertCaseInsensitiveSafe())
}
