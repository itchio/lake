package tlc

import (
	"fmt"
	"path"
	"strings"

	"github.com/pkg/errors"
)

type humanPrintable interface {
	ToString() string
}

// Validate verifies that the container doesn't contain wildly invalid
// stuff, like a directory and a file having the same name
func (container *Container) Validate() error {
	paths := make(map[string]humanPrintable)

	type errorBuffer struct {
		errors []string
	}
	buf := errorBuffer{}

	dup := func(a humanPrintable, b humanPrintable) {
		buf.errors = append(buf.errors, fmt.Sprintf("Two entries have the same name:\n%s\n%s", a.ToString(), b.ToString()))
	}

	for _, curr := range container.Files {
		if previous, ok := paths[curr.Path]; ok {
			dup(previous, curr)
		}
		paths[curr.Path] = curr
	}

	for _, curr := range container.Symlinks {
		if previous, ok := paths[curr.Path]; ok {
			dup(previous, curr)
		}
		paths[curr.Path] = curr
	}

	for _, curr := range container.Dirs {
		if previous, ok := paths[curr.Path]; ok {
			dup(previous, curr)
		}
		paths[curr.Path] = curr
	}

	// a file or symlink can never be the parent directory of another entry
	badParents := make(map[string]bool)
	checkAncestors := func(curr humanPrintable, currPath string) {
		for dir := path.Dir(currPath); dir != "" && dir != "." && dir != "/"; dir = path.Dir(dir) {
			parent, ok := paths[dir]
			if !ok {
				continue
			}
			switch parent.(type) {
			case *File, *Symlink:
				if !badParents[dir] {
					badParents[dir] = true
					buf.errors = append(buf.errors, fmt.Sprintf("An entry is used as the parent directory of other entries, but is not a directory:\n%s\n%s", parent.ToString(), curr.ToString()))
				}
			}
		}
	}

	for _, curr := range container.Files {
		checkAncestors(curr, curr.Path)
	}
	for _, curr := range container.Symlinks {
		checkAncestors(curr, curr.Path)
	}
	for _, curr := range container.Dirs {
		checkAncestors(curr, curr.Path)
	}

	if len(buf.errors) > 0 {
		return errors.New("Invalid container, found the following problems:\n" + strings.Join(buf.errors, "\n\n"))
	}
	return nil
}
