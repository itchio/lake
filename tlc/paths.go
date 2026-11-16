package tlc

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	maxContainerPathLen      = 1024
	maxContainerComponentLen = 255
	maxReportedPathProblems  = 10
	maxReportedPathBytes     = 200
)

// ErrUnsafePaths is returned by Validate when entries are unsafe to extract.
var ErrUnsafePaths = errors.New("container has unsafe paths")

// validatePaths rejects entries that are unsafe to extract: paths that are
// not clean and relative, that Windows can't represent, or that exceed length
// limits, entry modes that don't match their kind or carry setuid/setgid, and
// symlinks whose targets escape the container, including through
// case-insensitive or normalizing volumes.
func (c *Container) validatePaths() error {
	var problems []string
	report := func(kind, p, why string) {
		if len(problems) < maxReportedPathProblems {
			if len(p) > maxReportedPathBytes {
				p = p[:maxReportedPathBytes] + "..."
			}
			problems = append(problems, fmt.Sprintf("%s %q: %s", kind, p, why))
		} else if len(problems) == maxReportedPathProblems {
			problems = append(problems, "...")
		}
	}

	// only needed to check entries against link spellings below
	hasLinks := len(c.Symlinks) > 0
	type entry struct{ kind, path string }
	var entries []entry
	for _, f := range c.Files {
		if why := containerPathProblem(f.Path); why != "" {
			report("file", f.Path, why)
		} else if why := entryModeProblem(f.Mode, 0); why != "" {
			report("file", f.Path, why)
		} else if hasLinks {
			entries = append(entries, entry{"file", f.Path})
		}
	}
	for _, d := range c.Dirs {
		if why := containerPathProblem(d.Path); why != "" {
			report("dir", d.Path, why)
		} else if why := entryModeProblem(d.Mode, os.ModeDir); why != "" {
			report("dir", d.Path, why)
		} else if hasLinks {
			entries = append(entries, entry{"dir", d.Path})
		}
	}
	// The archive may land on a volume that ignores case, normalization or
	// trailing dots, where a target spelled "D/.." follows a link named "d".
	links := symlinkTable{folded: make(map[string][]string, len(c.Symlinks)), exact: make(map[string]bool, len(c.Symlinks))}
	for _, s := range c.Symlinks {
		if why := containerPathProblem(s.Path); why != "" {
			report("symlink", s.Path, why)
			continue
		}
		if why := entryModeProblem(s.Mode, os.ModeSymlink); why != "" {
			report("symlink", s.Path, why)
			continue
		}
		if why := symlinkTargetProblem(s.Dest); why != "" {
			report("symlink", s.Path+" -> "+s.Dest, why)
			continue
		}
		entries = append(entries, entry{"symlink", s.Path})
		key := foldPath(s.Path)
		links.folded[key] = append(links.folded[key], s.Dest)
		links.exact[s.Path] = true
	}
	// The walk below only finds links by where they are spelled. An entry
	// whose parent folds to a link, including the exact spelling, lands inside
	// that link's target, so a link placed there would be invisible to the
	// walk. This check must cover exact spellings too: the structural checks
	// in Validate that also reject them run after this function.
	if len(links.folded) > 0 {
		pc := parentChecker{links: links, folded: map[string]string{}, linked: map[string]string{}}
		for _, e := range entries {
			if parent := pc.linkedDir(path.Dir(e.path)); parent != "" {
				report(e.kind, e.path, fmt.Sprintf("parent %q may resolve to a symlink", parent))
			}
		}
	}
	// The walk's step budgets only hold when every target in the table has
	// passed the length and syntax checks, so walk only a container that
	// passed everything above.
	if len(problems) == 0 {
		total := maxSymlinkWalkStepsPerContainer
		for _, s := range c.Symlinks {
			if symlinkEscapes(links, s.Path, s.Dest, &total) {
				report("symlink", s.Path+" -> "+s.Dest, "symlink target escapes the container")
			}
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%w:\n%s", ErrUnsafePaths, strings.Join(problems, "\n"))
	}
	return nil
}

// The zip writer copies entry modes into the archive verbatim, so a File whose
// mode carries the symlink bit is extracted as a symlink to its contents, with
// none of the symlink checks applied. Only the bit for the entry's own kind is
// allowed; mode 0, which older containers carry, is fine. Setgid on a directory
// only makes new entries inherit its group, and builds made on CI runners and
// shared volumes routinely carry it.
func entryModeProblem(mode uint32, kind os.FileMode) string {
	m := os.FileMode(mode)
	privileged := os.ModeSetuid | os.ModeSetgid
	if kind == os.ModeDir {
		privileged = os.ModeSetuid
	}
	switch {
	case m&os.ModeType&^kind != 0:
		return fmt.Sprintf("mode %v is not a plain entry of its kind", m)
	case m&privileged != 0:
		return fmt.Sprintf("mode %v has setuid or setgid set", m)
	}
	return ""
}

func containerPathProblem(p string) string {
	switch {
	case len(p) > maxContainerPathLen:
		return fmt.Sprintf("longer than %d bytes", maxContainerPathLen)
	case p == "" || p == ".":
		return "empty path"
	case strings.ContainsRune(p, 0):
		return "contains NUL"
	case strings.Contains(p, `\`):
		return "contains backslash"
	case strings.HasPrefix(p, "/"):
		return "absolute path"
	case hasDriveLetter(p):
		return "starts with a drive letter"
	case strings.Contains(p, ":"):
		return "contains a colon"
	case path.Clean(p) != p:
		return "not a clean path"
	}
	for _, component := range strings.Split(p, "/") {
		switch {
		case component == "..":
			return `contains ".."`
		case isDotsComponent(component):
			return fmt.Sprintf("component %q is only dots and spaces", component)
		case len(component) > maxContainerComponentLen:
			return fmt.Sprintf("component longer than %d bytes", maxContainerComponentLen)
		case isWindowsDeviceName(component):
			return "component is a Windows device name"
		}
	}
	return ""
}

// parentChecker finds directories that fold to a symlink's path. Results and
// folded spellings are memoized per directory, so each directory in the
// container is folded once however many entries share it.
type parentChecker struct {
	links  symlinkTable
	folded map[string]string
	linked map[string]string
}

// linkedDir returns the outermost of dir and its ancestors that folds to a
// symlink's path, or "" if there is none. dir must be a clean relative path.
func (pc *parentChecker) linkedDir(dir string) string {
	if dir == "." {
		return ""
	}
	if result, ok := pc.linked[dir]; ok {
		return result
	}
	parent := path.Dir(dir)
	result := pc.linkedDir(parent)
	folded := foldComponent(path.Base(dir))
	if parent != "." {
		folded = pc.folded[parent] + "/" + folded
	}
	pc.folded[dir] = folded
	if result == "" {
		if _, ok := pc.links.folded[folded]; ok {
			result = dir
		}
	}
	pc.linked[dir] = result
	return result
}

// folded holds every spelling a lenient volume could resolve to a link;
// exact holds the one spelling a case-sensitive volume does.
type symlinkTable struct {
	folded map[string][]string
	exact  map[string]bool
}

func symlinkTargetProblem(dest string) string {
	switch {
	case len(dest) > maxContainerPathLen:
		return fmt.Sprintf("symlink target longer than %d bytes", maxContainerPathLen)
	case dest == "":
		return "empty symlink target"
	case strings.ContainsRune(dest, 0):
		return "symlink target contains NUL"
	case strings.Contains(dest, `\`):
		return "symlink target contains backslash"
	case strings.HasPrefix(dest, "/"):
		return "absolute symlink target"
	case hasDriveLetter(dest):
		return "symlink target starts with a drive letter"
	case strings.Contains(dest, ":"):
		return "symlink target contains a colon"
	}
	for _, component := range strings.Split(dest, "/") {
		// "", "." and ".." are walked as written; any other spelling made
		// only of dots and spaces resolves to one of them on Win32
		if component != "" && component != "." && component != ".." && isDotsComponent(component) {
			return fmt.Sprintf("symlink target component %q is only dots and spaces", component)
		}
	}
	return ""
}

const (
	// Same bound the kernel applies before returning ELOOP.
	maxSymlinkHops = 40
	// Components examined per link, summed across alias branches.
	maxSymlinkWalkSteps = 10000
)

// Components examined across all links of one container. Each step costs a
// path copy plus one component fold, so this bounds the whole walk to well
// under a second. A var so tests can lower it.
var maxSymlinkWalkStepsPerContainer = 2_000_000

// Components that name a link are replaced by its target as the walk goes, so
// ".." applies to where a component leads rather than how it is spelled. The
// link's own directory is walked from the root too: Container.Validate only
// guarantees that exact-spelling parents are directories, and an aliased
// spelling can be a link. Exhausting either budget counts as escaping.
func symlinkEscapes(links symlinkTable, linkPath, dest string, total *int) bool {
	var rest []string
	if dir := path.Dir(linkPath); dir != "." {
		rest = strings.Split(dir, "/")
	}
	rest = append(rest, strings.Split(dest, "/")...)
	perLink := maxSymlinkWalkSteps
	return walkEscapes(links, walkPos{}, rest, 0, &perLink, total)
}

// walkPos is the current directory as cumulative joined paths, one entry per
// depth, so a lookup key is a slice index rather than a re-join and re-fold
// of every component.
type walkPos struct {
	spelled, folded []string
}

func (w walkPos) depth() int { return len(w.spelled) }

func (w walkPos) push(component string) walkPos {
	sp, fd := component, foldComponent(component)
	if n := len(w.spelled); n > 0 {
		sp = w.spelled[n-1] + "/" + sp
		fd = w.folded[n-1] + "/" + fd
	}
	return walkPos{spelled: append(w.spelled, sp), folded: append(w.folded, fd)}
}

func (w walkPos) pop() walkPos {
	n := len(w.spelled) - 1
	return walkPos{spelled: w.spelled[:n], folded: w.folded[:n]}
}

func (w walkPos) clone() walkPos {
	return walkPos{spelled: append([]string(nil), w.spelled...), folded: append([]string(nil), w.folded...)}
}

func walkEscapes(links symlinkTable, pos walkPos, rest []string, hops int, perLink, total *int) bool {
	for len(rest) > 0 {
		*perLink--
		*total--
		if *perLink < 0 || *total < 0 {
			return true
		}
		component := rest[0]
		rest = rest[1:]
		switch component {
		case "", ".":
		case "..":
			if pos.depth() == 0 {
				return true
			}
			pos = pos.pop()
		default:
			pos = pos.push(component)
			top := pos.depth() - 1
			targets, ok := links.folded[pos.folded[top]]
			if !ok {
				continue
			}
			if hops+1 > maxSymlinkHops {
				return true
			}
			// On a case-sensitive volume this spelling is a link only if one
			// has exactly this spelling; otherwise it is a real entry there.
			literal := !links.exact[pos.spelled[top]]
			if len(targets) == 1 && !literal {
				hops++
				pos = pos.pop()
				rest = append(strings.Split(targets[0], "/"), rest...)
				continue
			}
			parent := pos.pop()
			for _, target := range targets {
				branch := append(strings.Split(target, "/"), rest...)
				if walkEscapes(links, parent.clone(), branch, hops+1, perLink, total) {
					return true
				}
			}
			if literal {
				return walkEscapes(links, pos.clone(), append([]string(nil), rest...), hops, perLink, total)
			}
			return false
		}
	}
	return false
}

var caseFolder = cases.Fold()

// NFKD over-folds relative to any real filesystem, which only adds aliases
// and so only adds rejections.
func foldComponent(c string) string {
	// ignorables go first so "d.‌" trims to the same key as "d."
	c = strings.TrimRight(strings.Map(dropHFSIgnorable, c), ". ")
	return strings.Map(foldHFSGeorgian, caseFolder.String(norm.NFKD.String(c)))
}

// HFS+ ignores these code points when comparing names, so "d‌" and "d"
// are the same entry there. This is every code point gLowerCaseTable in
// Apple's HFS sources maps to 0.
func dropHFSIgnorable(r rune) rune {
	switch {
	case r >= 0x200C && r <= 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x206A && r <= 0x206F, r == 0xFEFF:
		return -1
	}
	return r
}

// HFS+ predates Unicode's current Georgian case pairs: its table folds the
// capitals U+10A0..U+10C5 to U+10D0..U+10F5, where modern folding gives
// U+2D00..U+2D25. Mapping the modern result onto the HFS+ one after folding
// keeps both equivalences. Diffing gLowerCaseTable against this package's
// folding over the BMP finds no other disagreements.
func foldHFSGeorgian(r rune) rune {
	if r >= 0x2D00 && r <= 0x2D25 {
		return r - 0x2D00 + 0x10D0
	}
	return r
}

func foldPath(p string) string {
	components := strings.Split(p, "/")
	for i, c := range components {
		components[i] = foldComponent(c)
	}
	return strings.Join(components, "/")
}
