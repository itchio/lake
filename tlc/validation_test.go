package tlc_test

import (
	"os"
	"strings"
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
				Mode: 0o644,
				Size: 0,
			},
		},
		Dirs: []*tlc.Dir{
			&tlc.Dir{
				Path: "MonoBleedingEdge",
				Mode: 0o755 | uint32(os.ModeDir),
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
		Mode: 0o644 | uint32(os.ModeSymlink),
		Dest: "/etc/hosts",
	})

	err = c.Validate()
	assert.Error(err)
	t.Logf("As expected:\n%s", err)
}

func Test_ValidateFileUsedAsDir(t *testing.T) {
	assert := assert.New(t)

	// shape produced by WalkZip on a zip containing a zero-byte
	// "assets/assets" entry alongside files nested below it: the file
	// is never duplicated as a dir, it's only implied by descendants
	c := &tlc.Container{
		Files: []*tlc.File{
			{Path: "assets/assets", Mode: 0o644, Size: 0},
			{Path: "assets/assets/audio/bgm_act.ogg", Mode: 0o644, Size: 128},
			{Path: "assets/assets/data/level1.json", Mode: 0o644, Size: 64},
		},
		Dirs: []*tlc.Dir{
			{Path: "assets", Mode: 0o755 | uint32(os.ModeDir)},
			{Path: "assets/assets/audio", Mode: 0o755 | uint32(os.ModeDir)},
			{Path: "assets/assets/data", Mode: 0o755 | uint32(os.ModeDir)},
		},
	}

	err := c.Validate()
	assert.Error(err)
	t.Logf("As expected:\n%s", err)

	// the offending file should only be reported once, not per descendant
	assert.Equal(1, strings.Count(err.Error(), "is used as the parent directory"))

	// legit nesting under an actual dir is fine
	c.Files[0].Path = "assets/assets/extra.bin"
	c.Dirs = append(c.Dirs, &tlc.Dir{
		Path: "assets/assets",
		Mode: 0o755 | uint32(os.ModeDir),
	})
	assert.NoError(c.Validate())
}

func Test_ValidateSymlinkUsedAsDir(t *testing.T) {
	assert := assert.New(t)

	c := &tlc.Container{
		Files: []*tlc.File{
			{Path: "foo/bar.txt", Mode: 0o644, Size: 12},
		},
		Symlinks: []*tlc.Symlink{
			{Path: "foo", Mode: 0o644 | uint32(os.ModeSymlink), Dest: "elsewhere"},
		},
	}

	err := c.Validate()
	assert.Error(err)
	t.Logf("As expected:\n%s", err)
}
