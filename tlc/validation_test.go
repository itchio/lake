package tlc_test

import (
	"os"
	"testing"

	"github.com/itchio/lake/tlc"
	"github.com/stretchr/testify/assert"
)

func Test_Validate(t *testing.T) {
	assert := assert.New(t)

	c := &tlc.Container{
		Files: []*tlc.File{
			&tlc.File{
				Path: "MonoBleedingEdge",
				Mode: 0644,
				Size: 0,
			},
		},
		Dirs: []*tlc.Dir{
			&tlc.Dir{
				Path: "MonoBleedingEdge",
				Mode: 0755 | uint32(os.ModeDir),
			},
		},
	}

	var err error

	err = c.Validate()
	assert.Error(err)
	t.Logf("As expected:\n%s", err)

	c.Dirs[0].Path = "NowADir"
	err = c.Validate()
	assert.NoError(err)

	c.Symlinks = append(c.Symlinks, &tlc.Symlink{
		Path: "MonoBleedingEdge",
		Mode: 0644 | uint32(os.ModeSymlink),
		Dest: "/etc/hosts",
	})

	err = c.Validate()
	assert.Error(err)
	t.Logf("As expected:\n%s", err)
}
