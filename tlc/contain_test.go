package tlc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestContainedPathLexical(t *testing.T) {
	base := t.TempDir()
	for _, p := range []string{"a.txt", "d/e/f.txt", "..hidden", "a.b.c", "trailing."} {
		if _, err := ContainedPath(base, p); err != nil {
			t.Errorf("%q rejected: %v", p, err)
		}
	}
	for _, p := range []string{
		"", ".", "./", "./.", ".//.", "..", "../x", "d/../../x", "d/./x", "a/", "a//b",
		"/etc/x", `d\x`, "C:/x", "a\x00b", ".. ", "a/.../b", "d/.. /x", "NUL", "data/con.txt", "com1",
	} {
		got, err := ContainedPath(base, p)
		if !errors.Is(err, ErrPathEscape) {
			t.Errorf("%q accepted as %q (%v)", p, got, err)
		}
	}
}

func TestContainedPathNeverReturnsBase(t *testing.T) {
	base := t.TempDir()
	abs, _ := filepath.Abs(base)
	for _, p := range []string{".", "./", "x/..", "./x/.."} {
		if got, err := ContainedPath(base, p); err == nil && got == abs {
			t.Fatalf("%q resolved to the base itself", p)
		}
	}
}

func TestContainedPathRootBase(t *testing.T) {
	root := string(filepath.Separator)
	got, err := ContainedPath(root, "etc/x")
	if err != nil || got != filepath.Join(root, "etc", "x") {
		t.Fatalf("root base: %q %v", got, err)
	}
}

func TestContainedPathSymlinkParent(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "d")); err != nil {
		t.Skip(err)
	}
	os.MkdirAll(filepath.Join(base, "real/sub"), 0o755)

	if _, err := ContainedPath(base, "d/e.txt"); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("write through symlinked parent accepted (%v)", err)
	}
	if _, err := ContainedPath(base, "real/sub/e.txt"); err != nil {
		t.Fatalf("real parent rejected: %v", err)
	}
	if _, err := ContainedPath(base, "real/missing/deeper/e.txt"); err != nil {
		t.Fatalf("not-yet-created parent rejected: %v", err)
	}
	// Replacing the leaf symlink itself is the caller's business.
	if _, err := ContainedPath(base, "d"); err != nil {
		t.Fatalf("leaf symlink rejected: %v", err)
	}
}

func TestContainedFilePathRefusesLeafSymlink(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("x"), 0o600)
	if err := os.Symlink(outside, filepath.Join(base, "f")); err != nil {
		t.Skip(err)
	}
	os.WriteFile(filepath.Join(base, "g"), []byte("y"), 0o600)
	if _, err := ContainedFilePath(base, "f"); !errors.Is(err, ErrPathEscape) {
		t.Fatalf("leaf symlink accepted (%v)", err)
	}
	if _, err := ContainedFilePath(base, "g"); err != nil {
		t.Fatalf("regular file rejected: %v", err)
	}
	if _, err := ContainedFilePath(base, "missing"); err != nil {
		t.Fatalf("missing file rejected: %v", err)
	}
}

func TestContainedSymlinkTarget(t *testing.T) {
	base := t.TempDir()
	link := filepath.Join(base, "lib", "x")
	for _, dest := range []string{"y", "../bin/z", "sub/../y", "."} {
		if err := ContainedSymlinkTarget(base, link, dest); err != nil {
			t.Errorf("%q rejected: %v", dest, err)
		}
	}
	for _, dest := range []string{"", "../../out", "/etc/hostname", "/Windows/x", `..\out`, "C:/x"} {
		if err := ContainedSymlinkTarget(base, link, dest); !errors.Is(err, ErrPathEscape) {
			t.Errorf("%q accepted (%v)", dest, err)
		}
	}
}

func TestPrepareRefusesEscapes(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "install")
	victim := filepath.Join(parent, "victim.txt")
	os.WriteFile(victim, []byte("keep me"), 0o644)

	for _, c := range []*Container{
		{Files: []*File{{Path: "../victim.txt", Mode: 0o644, Size: 0}}},
		{Dirs: []*Dir{{Path: "../newdir", Mode: 0o755 | uint32(os.ModeDir)}}},
		{Symlinks: []*Symlink{{Path: "../victim.txt", Dest: "x", Mode: 0o777 | uint32(os.ModeSymlink)}}},
		{Symlinks: []*Symlink{{Path: "link", Dest: "/etc/hostname", Mode: 0o777 | uint32(os.ModeSymlink)}}},
	} {
		if err := c.Prepare(base); !errors.Is(err, ErrPathEscape) {
			t.Errorf("accepted: %+v (%v)", c, err)
		}
	}
	if data, _ := os.ReadFile(victim); string(data) != "keep me" {
		t.Fatal("file outside the base was modified")
	}
	if _, err := os.Lstat(filepath.Join(parent, "newdir")); !os.IsNotExist(err) {
		t.Fatal("directory created outside the base")
	}
	if _, err := os.Lstat(filepath.Join(base, "link")); !os.IsNotExist(err) {
		t.Fatal("absolute symlink was created")
	}
}

func TestPrepareDirReplacesForeignLeafSymlink(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Chmod(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "d")); err != nil {
		t.Skip(err)
	}
	c := &Container{Dirs: []*Dir{{Path: "d", Mode: 0o777 | uint32(os.ModeDir)}}}
	if err := c.Prepare(base); err != nil {
		t.Fatal(err)
	}
	if stat, _ := os.Stat(outside); stat.Mode().Perm() != 0o700 {
		t.Fatalf("prepare chmodded through the symlink: %v", stat.Mode())
	}
	if stat, err := os.Lstat(filepath.Join(base, "d")); err != nil || !stat.IsDir() {
		t.Fatal("symlink was not replaced by a directory")
	}
}

func TestPrepareReplacesForeignLeafSymlink(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("secret"), 0o600)
	if err := os.Symlink(outside, filepath.Join(base, "f")); err != nil {
		t.Skip(err)
	}
	c := &Container{Files: []*File{{Path: "f", Mode: 0o644, Size: 0}}}
	if err := c.Prepare(base); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(outside); string(data) != "secret" {
		t.Fatal("prepare wrote through the symlink")
	}
	if stat, err := os.Lstat(filepath.Join(base, "f")); err != nil || stat.Mode()&os.ModeSymlink != 0 {
		t.Fatal("symlink was not replaced by a regular file")
	}
}
