package tlc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrPathEscape is returned when a container path would resolve outside the
// directory it is being written to or read from.
var ErrPathEscape = errors.New("path escapes its base directory")

// ContainedPath is what pools and bowls call before touching disk for a
// container entry. The lexical check catches "..", absolute paths and
// separators the container format forbids. The on-disk check refuses to go
// through a parent that is currently a symlink: once an earlier entry has
// created "d" -> ".", a later "d/e" is really "e", and "d/e" -> ".." then
// escapes the base, whatever the lexical form says. The leaf itself may be a
// symlink; callers that replace it use this, callers that open it use
// ContainedFilePath.
func ContainedPath(base, containerPath string) (string, error) {
	if err := ValidatePath(containerPath); err != nil {
		return "", err
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	joined := filepath.Join(absBase, filepath.FromSlash(containerPath))
	if joined == absBase || !within(absBase, joined) {
		return "", fmt.Errorf("%w: %s", ErrPathEscape, containerPath)
	}
	if err := rejectSymlinkParents(absBase, joined); err != nil {
		return "", err
	}
	return joined, nil
}

// ContainedFilePath is ContainedPath for a path that will be opened in place:
// it also refuses a leaf that is currently a symlink, since opening would
// follow it wherever it points. Container files are never symlinks, so a
// symlink at a file's path is foreign.
func ContainedFilePath(base, containerPath string) (string, error) {
	p, err := ContainedPath(base, containerPath)
	if err != nil {
		return "", err
	}
	stat, err := os.Lstat(p)
	if err == nil && stat.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: %s is a symlink", ErrPathEscape, containerPath)
	}
	return p, nil
}

// ContainedSymlinkTarget checks a symlink that will be created at
// linkDiskPath (as returned by ContainedPath) with target dest. Absolute and
// rooted targets, and targets that resolve lexically above base, are refused.
// This is a lexical check only; what finally protects against a target that
// escapes through another link is that every later open refuses symlinked
// parents and symlinked leaves.
func ContainedSymlinkTarget(base, linkDiskPath, dest string) error {
	if dest == "" || strings.ContainsRune(dest, 0) || strings.Contains(dest, `\`) ||
		strings.HasPrefix(dest, "/") || filepath.IsAbs(dest) || hasDriveLetter(dest) {
		return fmt.Errorf("%w: symlink target %q", ErrPathEscape, dest)
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	resolved := filepath.Join(filepath.Dir(linkDiskPath), filepath.FromSlash(dest))
	if !within(absBase, resolved) {
		return fmt.Errorf("%w: symlink target %q", ErrPathEscape, dest)
	}
	return nil
}

// ValidatePath is the lexical half of ContainedPath: a relative, slash
// separated path with no empty, "." or ".." components. Trailing dots and
// spaces are trimmed first because Win32 drops them, so ".. " is "..".
func ValidatePath(p string) error {
	switch {
	case p == "":
		return fmt.Errorf("%w: empty path", ErrPathEscape)
	case strings.ContainsRune(p, 0), strings.Contains(p, `\`), strings.HasPrefix(p, "/"), hasDriveLetter(p):
		return fmt.Errorf("%w: %q", ErrPathEscape, p)
	}
	for _, component := range strings.Split(p, "/") {
		switch strings.TrimRight(component, ". ") {
		case "", ".", "..":
			return fmt.Errorf("%w: %q", ErrPathEscape, p)
		}
		if isWindowsDeviceName(component) {
			return fmt.Errorf("%w: %q names a Windows device", ErrPathEscape, p)
		}
	}
	return nil
}

func within(absBase, p string) bool {
	prefix := absBase
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return p == absBase || strings.HasPrefix(p, prefix)
}

func rejectSymlinkParents(absBase, p string) error {
	rel, err := filepath.Rel(absBase, filepath.Dir(p))
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	cur := absBase
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, component)
		stat, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if stat.Mode()&os.ModeSymlink != 0 {
			relLink, _ := filepath.Rel(absBase, cur)
			return fmt.Errorf("%w: parent %s is a symlink", ErrPathEscape, relLink)
		}
	}
	return nil
}

func hasDriveLetter(p string) bool {
	return len(p) >= 2 && p[1] == ':' &&
		(('a' <= p[0] && p[0] <= 'z') || ('A' <= p[0] && p[0] <= 'Z'))
}

// Win32 treats these basenames as devices in any directory and with any
// extension; a write to them never reaches a file.
func isWindowsDeviceName(component string) bool {
	base := strings.ToUpper(component)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		return '1' <= base[3] && base[3] <= '9'
	}
	return false
}
