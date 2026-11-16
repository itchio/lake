package tlc

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
)

func files(paths ...string) []*File {
	var out []*File
	for _, p := range paths {
		out = append(out, &File{Path: p})
	}
	return out
}

func link(p, dest string) *Symlink { return &Symlink{Path: p, Dest: dest} }

func TestValidatePathsAccepts(t *testing.T) {
	good := &Container{
		Files: append(files("index.html", "data/version.txt", "Game.app/Contents/MacOS/Game", "Data/y", "a.b.c", "..hidden", "trailing."),
			&File{Path: "bin/game", Mode: 0o755}),
		Dirs: []*Dir{{Path: "data", Mode: 0o755 | uint32(os.ModeDir)}, {Path: "Data"}, {Path: "Game.app/Contents/MacOS"},
			{Path: "assets", Mode: 0o774 | uint32(os.ModeDir|os.ModeSetgid)}},
		Symlinks: []*Symlink{
			{Path: "Game.app/Contents/Current", Dest: "Versions/A", Mode: 0o777 | uint32(os.ModeSymlink)},
			link("Game.app/Contents/Foo", "Current/Foo"),
			link("lib/libfoo.so", "libfoo.so.1"),
			link("bin/tool", "../lib/tool"),
			link("lib64", "lib"),
			link("bin/tool2", "../lib64/../lib/tool"),
			link("deep", "data/sub"),
			link("bin/tool3", "../deep/../version.txt"),
			link("Data/x", "../data/y"),
			// "Data2" is a real directory on a strict volume and link "data2"
			// on a lenient one; both stay inside.
			link("data2", "x"),
			link("e2", "Data2/../y"),
			link("missing", "nowhere/at/all"),
			link("lib32", "lib/"),
			link("Foo.framework/Versions/Current", "A"),
			link("Foo.framework/Resources", "Versions/Current/Resources"),
		},
	}
	good.Files = append(good.Files, files("Foo.framework/Versions/A/Resources/Info.plist", "data./x", "a .b", "console.txt", "com10", "lpt0", "nullable.txt", "auxiliary/x")...)
	if err := good.validatePaths(); err != nil {
		t.Fatalf("good container rejected: %v", err)
	}
}

func TestValidatePathsRejects(t *testing.T) {
	deep := strings.Repeat("a/", 600) + "x.txt"
	bad := []struct {
		name string
		c    *Container
	}{
		{"file ..", &Container{Files: files("../poc.txt")}},
		{"dir ..", &Container{Dirs: []*Dir{{Path: ".."}}}},
		{"nested ..", &Container{Files: files("data/../../poc.txt")}},
		{"absolute", &Container{Files: files("/etc/poc")}},
		{"backslash", &Container{Files: files(`..\poc.txt`)}},
		{"drive letter", &Container{Files: files(`C:/poc.txt`)}},
		{"colon", &Container{Files: files("a.txt:stream")}},
		{"device name", &Container{Files: files("data/CON.txt")}},
		{"device name lower", &Container{Files: files("nul")}},
		{"device name com", &Container{Dirs: []*Dir{{Path: "com1"}}}},
		{"unclean", &Container{Files: files("./poc.txt")}},
		{"empty", &Container{Files: files("")}},
		{"NUL", &Container{Files: files("a\x00b")}},
		{"long path", &Container{Files: files(deep)}},
		{"long component", &Container{Files: files(strings.Repeat("a", 256))}},
		{"symlink path ..", &Container{Symlinks: []*Symlink{link("../link", "x")}}},
		{"absolute symlink target", &Container{Symlinks: []*Symlink{link("link", "/etc/hostname")}}},
		{"symlink target colon", &Container{Symlinks: []*Symlink{link("link", "ab:c")}}},
		{"escaping symlink target", &Container{Symlinks: []*Symlink{link("link", "../outside")}}},
		{"escaping nested symlink target", &Container{Symlinks: []*Symlink{link("a/b/link", "../../../outside")}}},
		{"empty symlink target", &Container{Symlinks: []*Symlink{link("link", "")}}},
		{"escape through a symlink to the root", &Container{Symlinks: []*Symlink{link("d", "."), link("e", "d/../outside.txt")}}},
		{"escape through a chain", &Container{Symlinks: []*Symlink{link("a", "b"), link("b", "."), link("e", "a/../outside.txt")}}},
		{"escape through a nested symlink", &Container{Symlinks: []*Symlink{link("x/up", ".."), link("x/e", "up/../outside.txt")}}},
		{"escape through a case-insensitive lookup", &Container{Symlinks: []*Symlink{link("d", "."), link("e", "D/../outside.txt")}}},
		{"escape through one of two case-colliding links", &Container{Symlinks: []*Symlink{link("A", "."), link("a", "sub"), link("e", "a/../outside.txt")}}},
		{"escape through a case-variant parent link", &Container{Files: files("x/UP/poc.txt"), Symlinks: []*Symlink{link("X", "."), link("x/up", "..")}}},
		{"escape through a case-variant parent chain", &Container{Symlinks: []*Symlink{link("a/b", "../c"), link("a/B/q", "../../x")}}},
		{"escape through NFC/NFD aliasing", &Container{Symlinks: []*Symlink{link("caf\u00e9", "."), link("e", "cafe\u0301/../outside")}}},
		{"escape through full case folding", &Container{Symlinks: []*Symlink{link("\u017f", "."), link("e", "s/../outside")}}},
		{"escape through trailing dot stripping", &Container{Symlinks: []*Symlink{link("d", "."), link("e", "d./../outside")}}},
		{"escape via the literal spelling of a folded link", &Container{Dirs: []*Dir{{Path: "D"}}, Symlinks: []*Symlink{link("d", "sub/deep"), link("e", "D/../../outside.txt")}}},
		{"file with symlink mode", &Container{Files: []*File{{Path: "escape", Mode: 0o777 | uint32(os.ModeSymlink)}}}},
		{"dir with symlink mode", &Container{Dirs: []*Dir{{Path: "escape", Mode: 0o777 | uint32(os.ModeSymlink)}}}},
		{"symlink with dir mode", &Container{Symlinks: []*Symlink{{Path: "x", Dest: "y", Mode: 0o755 | uint32(os.ModeDir)}}}},
		{"file with device mode", &Container{Files: []*File{{Path: "dev", Mode: 0o644 | uint32(os.ModeDevice)}}}},
		{"setuid file", &Container{Files: []*File{{Path: "bin/game", Mode: 0o755 | uint32(os.ModeSetuid)}}}},
		{"setgid file", &Container{Files: []*File{{Path: "bin/game", Mode: 0o755 | uint32(os.ModeSetgid)}}}},
		{"setuid dir", &Container{Dirs: []*Dir{{Path: "bin", Mode: 0o755 | uint32(os.ModeDir|os.ModeSetuid)}}}},
		{"self loop", &Container{Symlinks: []*Symlink{link("a", "a")}}},
		{"two link cycle", &Container{Symlinks: []*Symlink{link("a", "b"), link("b", "a")}}},
		{"escape through a link inside an aliased parent link", &Container{Dirs: []*Dir{{Path: "dir"}}, Symlinks: []*Symlink{link("a", "dir"), link("A/b", ".."), link("e", "a/b/../outside")}}},
		{"file inside an aliased parent link", &Container{Files: files("A/x"), Symlinks: []*Symlink{link("a", "dir")}}},
		{"dots and space component", &Container{Files: files(".. /outside")}},
		{"three dots component", &Container{Files: files(".../outside")}},
		{"space only component", &Container{Files: files(" /x")}},
		{"dots and space symlink target", &Container{Symlinks: []*Symlink{link("x/link", ".. /.. /outside")}}},
		{"three dots symlink target", &Container{Symlinks: []*Symlink{link("link", ".../x")}}},
		{"device name trailing space", &Container{Files: files("nul ")}},
		{"device name trailing dot", &Container{Files: files("data/com1.")}},
		{"device name superscript digit", &Container{Files: files("com\u00b9.txt")}},
		{"console device name", &Container{Files: files("CONIN$")}},
		{"escape through an HFS+ ignorable code point", &Container{Symlinks: []*Symlink{link("d", "."), link("e", "d\u200c/../outside")}}},
		{"escape through an HFS+ ignorable after a trailing dot", &Container{Symlinks: []*Symlink{link("d.", "."), link("e", "d.\u200c/../outside")}}},
		{"escape through an HFS+ Georgian case pair", &Container{Symlinks: []*Symlink{link("\u10a0", "."), link("e", "\u10d0/../outside")}}},
		{"escape through a modern Georgian case pair", &Container{Symlinks: []*Symlink{link("\u10a0", "."), link("e", "\u2d00/../outside")}}},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.c.validatePaths(); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func TestValidatePathsBoundsReport(t *testing.T) {
	c := &Container{}
	for i := 0; i < 100; i++ {
		c.Files = append(c.Files, &File{Path: "../poc"})
	}
	c.Files = append(c.Files, &File{Path: strings.Repeat("a", 5<<20) + `\`})
	err := c.validatePaths()
	if err == nil {
		t.Fatal("accepted")
	}
	if n := strings.Count(err.Error(), "\n"); n > maxReportedPathProblems+1 {
		t.Fatalf("report has %d lines", n)
	}
	if len(err.Error()) > 16*1024 {
		t.Fatalf("report is %d bytes", len(err.Error()))
	}
}

// Forty two-way case collisions in a chain would be 2^40 walks without the
// step budget.
func TestSymlinkWalkBudget(t *testing.T) {
	c := &Container{}
	for i := 0; i < 40; i++ {
		next := fmt.Sprintf("l%d", i+1)
		c.Symlinks = append(c.Symlinks, link(fmt.Sprintf("l%d", i), next), link(fmt.Sprintf("L%d", i), next))
	}
	c.Symlinks = append(c.Symlinks, link("e", "l0/x"))
	if err := c.validatePaths(); err == nil {
		t.Fatal("accepted")
	}
}

// Many cheap links must share one container-wide budget, so link count alone
// cannot make the walk expensive.
func TestSymlinkWalkContainerBudget(t *testing.T) {
	saved := maxSymlinkWalkStepsPerContainer
	maxSymlinkWalkStepsPerContainer = 50
	defer func() { maxSymlinkWalkStepsPerContainer = saved }()

	c := &Container{}
	for i := 0; i < 100; i++ {
		c.Symlinks = append(c.Symlinks, link(fmt.Sprintf("l%d", i), "a/b/c"))
	}
	if err := c.validatePaths(); err == nil {
		t.Fatal("accepted")
	}
}

func TestValidateReportsUnsafePaths(t *testing.T) {
	c := &Container{Files: files("data/save:1.txt")}
	if err := c.Validate(); !errors.Is(err, ErrUnsafePaths) {
		t.Fatalf("expected ErrUnsafePaths, got %v", err)
	}
}

// An oversized target must be rejected before other links expand it during
// the walk, which allocates per reference before any step budget applies.
func TestOversizedTargetNotWalked(t *testing.T) {
	c := &Container{Symlinks: []*Symlink{link("big", strings.Repeat("a/", 1<<19))}}
	for i := 0; i < 8; i++ {
		c.Symlinks = append(c.Symlinks, link(fmt.Sprintf("r%d", i), "big/x"))
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	err := c.validatePaths()
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("accepted")
	}
	if n := after.TotalAlloc - before.TotalAlloc; n > 16<<20 {
		t.Fatalf("validation allocated %d MiB", n>>20)
	}
}

// Entries sharing deep directories must not refold every ancestor per entry.
func TestDeepParentsCheckedOnce(t *testing.T) {
	dir := strings.Repeat("d/", 500)
	c := &Container{Symlinks: []*Symlink{link("link", "d")}}
	for i := 0; i < 2000; i++ {
		c.Files = append(c.Files, &File{Path: fmt.Sprintf("%sf%d", dir, i)})
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if err := c.validatePaths(); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if n := after.TotalAlloc - before.TotalAlloc; n > 64<<20 {
		t.Fatalf("validation allocated %d MiB", n>>20)
	}
}
