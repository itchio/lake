package tlc

import (
	"strings"

	"github.com/pkg/errors"
)

// AssertCaseInsensitiveSafe returns an error if there
// exists multiple entries that differ only by their casing,
// like `foo/bar` and `foo/BAR`
func (c *Container) AssertCaseInsensitiveSafe() error {
	paths := make(map[string]string)

	var err error

	c.ForEachEntry(func(e Entry) ForEachOutcome {
		lowerPath := strings.ToLower(e.GetPath())
		if otherPath, ok := paths[lowerPath]; ok {
			err = errors.Errorf("Case conflict betwen (%s) and (%s)", otherPath, e.GetPath())
			return ForEachBreak
		}
		paths[lowerPath] = e.GetPath()
		return ForEachContinue
	})

	return err
}
