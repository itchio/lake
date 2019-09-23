package tlc

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
)

type humanPrintable interface {
	ToString() string
}

// CheckSanity verifies that the container doesn't contain wildly invalid
// stuff, like a directory and a file having the same name
func (container *Container) CheckSanity() error {
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

	if len(buf.errors) > 0 {
		return errors.New("Invalid container, found the following problems:\n" + strings.Join(buf.errors, "\n\n"))
	}
	return nil
}
