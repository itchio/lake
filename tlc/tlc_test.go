package tlc

import (
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/itchio/arkive/zip"
	"github.com/pkg/errors"

	"github.com/itchio/headway/state"
	"github.com/stretchr/testify/assert"
)

func Test_NonDirWalk(t *testing.T) {
	tmpPath, err := ioutil.TempDir("", "nondirwalk")
	must(t, err)
	defer os.RemoveAll(tmpPath)

	foobarPath := path.Join(tmpPath, "foobar")
	f, err := os.Create(foobarPath)
	must(t, err)
	must(t, f.Close())

	_, err = WalkDir(f.Name(), WalkOpts{})
	assert.NotNil(t, err, "should refuse to walk non-directory")
}

func Test_WalkZip(t *testing.T) {
	tmpPath := mktestdir(t, "walkzip")
	defer os.RemoveAll(tmpPath)

	tmpPath2, err := ioutil.TempDir("", "walkzip2")
	must(t, err)
	defer os.RemoveAll(tmpPath2)

	container, err := WalkDir(tmpPath, WalkOpts{})
	must(t, err)

	zipPath := path.Join(tmpPath2, "container.zip")
	zipWriter, err := os.Create(zipPath)
	must(t, err)
	defer zipWriter.Close()

	err = compressZip(zipWriter, tmpPath, &state.Consumer{})
	must(t, err)

	zipSize, err := zipWriter.Seek(0, io.SeekCurrent)
	must(t, err)

	zipReader, err := zip.NewReader(zipWriter, zipSize)
	must(t, err)

	zipContainer, err := WalkZip(zipReader, WalkOpts{})
	must(t, err)

	if testSymlinks {
		assert.Equal(t, "5 files, 3 dirs, 2 symlinks", container.Stats(), "should report correct stats")
	} else {
		assert.Equal(t, "5 files, 3 dirs, 0 symlinks", container.Stats(), "should report correct stats")
	}

	totalSize := int64(0)
	for _, regular := range regulars {
		totalSize += int64(regular.Size)
	}
	assert.Equal(t, totalSize, container.Size, "should report correct size")

	must(t, container.EnsureEqual(zipContainer))
}

func Test_Walk(t *testing.T) {
	tmpPath := mktestdir(t, "walk")
	defer os.RemoveAll(tmpPath)

	container, err := WalkDir(tmpPath, WalkOpts{})
	must(t, err)

	dirs := []string{
		"foo",
		"foo/dir_a",
		"foo/dir_b",
	}
	for i, dir := range dirs {
		assert.Equal(t, dir, container.Dirs[i].Path, "dirs should be all listed")
	}

	files := []string{
		"foo/dir_a/baz",
		"foo/dir_a/bazzz",
		"foo/dir_b/zoom",
		"foo/file_f",
		"foo/file_z",
	}
	for i, file := range files {
		assert.Equal(t, file, container.Files[i].Path, "files should be all listed")
	}

	if testSymlinks {
		for i, symlink := range symlinks {
			assert.Equal(t, symlink.Newname, container.Symlinks[i].Path, "symlink should be at correct path")
			assert.Equal(t, symlink.Oldname, container.Symlinks[i].Dest, "symlink should point to correct path")
		}
	}

	if testSymlinks {
		assert.Equal(t, "5 files, 3 dirs, 2 symlinks", container.Stats(), "should report correct stats")
	} else {
		assert.Equal(t, "5 files, 3 dirs, 0 symlinks", container.Stats(), "should report correct stats")
	}

	totalSize := int64(0)
	for _, regular := range regulars {
		totalSize += int64(regular.Size)
	}
	assert.Equal(t, totalSize, container.Size, "should report correct size")

	if testSymlinks {
		container, err := WalkDir(tmpPath, WalkOpts{Dereference: true})
		must(t, err)

		assert.EqualValues(t, 0, len(container.Symlinks), "when dereferencing, no symlinks should be listed")

		files := []string{
			"foo/dir_a/baz",
			"foo/dir_a/bazzz",
			"foo/dir_b/zoom",
			"foo/file_f",
			"foo/file_m",
			"foo/file_o",
			"foo/file_z",
		}
		for i, file := range files {
			assert.Equal(t, file, container.Files[i].Path, "when dereferencing, symlinks should appear as files")
		}

		// add both dereferenced symlinks to total size
		totalSize += int64(regulars[3].Size) // foo/file_z
		totalSize += int64(regulars[1].Size) // foo/dir_a/baz
		assert.Equal(t, totalSize, container.Size, "when dereferencing, should report correct size")
	}
}

func Test_WalkIgnore(t *testing.T) {
	assert := assert.New(t)

	t.Logf("=========== Dir")

	tmpPath := mktestdir(t, "walk")
	defer os.RemoveAll(tmpPath)

	must(t, os.MkdirAll(filepath.Join(tmpPath, ".itch", "tmp"), 0o755))
	must(t, ioutil.WriteFile(filepath.Join(tmpPath, ".itch", "tmp", "garbage"), []byte{0x1}, 0o644))
	must(t, ioutil.WriteFile(filepath.Join(tmpPath, ".DS_Store"), []byte{0x2}, 0o644))

	must(t, os.MkdirAll(filepath.Join(tmpPath, "subdir"), 0o755))
	must(t, ioutil.WriteFile(filepath.Join(tmpPath, "subdir", ".DS_Store"), []byte{0x3}, 0o644))
	must(t, ioutil.WriteFile(filepath.Join(tmpPath, "subdir", "Thumbs.db"), []byte{0x3}, 0o644))
	must(t, ioutil.WriteFile(filepath.Join(tmpPath, "subdir", "._01234"), []byte{0x99}, 0o644))

	must(t, os.MkdirAll(filepath.Join(tmpPath, "subdir", ".hg"), 0o755))
	must(t, os.MkdirAll(filepath.Join(tmpPath, "subdir", ".svn"), 0o755))

	must(t, os.MkdirAll(filepath.Join(tmpPath, "subdir", ".git"), 0o755))
	must(t, ioutil.WriteFile(filepath.Join(tmpPath, "subdir", ".git", "HEAD"), []byte("🤕"), 0o644))

	verifyContainer := func(container *Container) {
		forbiddenWords := []string{".itch", ".DS_Store", ".git", ".svn", ".hg", "HEAD", "Thumbs.db", "._01234"}
		for _, fw := range forbiddenWords {
			for _, f := range container.Files {
				assert.False(strings.Contains(f.Path, fw), "file %q should have been filtered out (%q)", fw)
			}
			for _, f := range container.Dirs {
				assert.False(strings.Contains(f.Path, fw), "dir %q should have been filtered out (%q)", fw)
			}
		}
	}

	var loggingFilter FilterFunc = func(name string) FilterResult {
		res := PresetFilter(name)
		if res == FilterIgnore {
			t.Logf("Ignoring %q", name)
		}
		return res
	}
	walkOpts := WalkOpts{
		Filter: loggingFilter,
	}

	container, err := WalkDir(tmpPath, walkOpts)
	must(t, err)
	verifyContainer(container)

	t.Logf("=========== Archive")

	archiveDir, err := ioutil.TempDir("", "tmp_archive")
	must(t, err)
	must(t, os.RemoveAll(archiveDir))
	must(t, os.MkdirAll(archiveDir, 0o755))
	defer os.RemoveAll(archiveDir)

	archive, err := os.Create(filepath.Join(archiveDir, "archive.zip"))
	must(t, err)
	archiveName := archive.Name()
	defer archive.Close()

	must(t, compressZip(archive, tmpPath, &state.Consumer{}))
	archive.Close()

	container, err = WalkAny(archiveName, walkOpts)
	must(t, err)
	verifyContainer(container)
}

func Test_Prepare(t *testing.T) {
	tmpPath := mktestdir(t, "prepare")
	defer os.RemoveAll(tmpPath)

	container, err := WalkDir(tmpPath, WalkOpts{})
	must(t, err)

	tmpPath2, err := ioutil.TempDir("", "prepare")
	defer os.RemoveAll(tmpPath2)
	must(t, err)

	err = container.Prepare(tmpPath2)
	must(t, err)

	container2, err := WalkDir(tmpPath2, WalkOpts{})
	must(t, err)

	must(t, container.EnsureEqual(container2))

	container3 := container2.Clone()
	must(t, container2.EnsureEqual(container3))
}

// Support code

func must(t *testing.T, err error) {
	if err != nil {
		t.Error("must failed: ", err.Error())
		t.FailNow()
	}
}

type regEntry struct {
	Path string
	Size int
	Byte byte
}

type symlinkEntry struct {
	Oldname string
	Newname string
}

var regulars = []regEntry{
	{"foo/file_f", 50, 0xd},
	{"foo/dir_a/baz", 10, 0xa},
	{"foo/dir_b/zoom", 30, 0xc},
	{"foo/file_z", 40, 0xe},
	{"foo/dir_a/bazzz", 20, 0xb},
}

var symlinks = []symlinkEntry{
	{"file_z", "foo/file_m"},
	{"dir_a/baz", "foo/file_o"},
}

var testSymlinks = runtime.GOOS != "windows"

func mktestdir(t *testing.T, name string) string {
	tmpPath, err := ioutil.TempDir("", "tmp_"+name)
	must(t, err)

	must(t, os.RemoveAll(tmpPath))

	for _, entry := range regulars {
		fullPath := filepath.Join(tmpPath, entry.Path)
		must(t, os.MkdirAll(filepath.Dir(fullPath), os.FileMode(0o777)))
		file, err := os.Create(fullPath)
		must(t, err)

		filler := []byte{entry.Byte}
		for i := 0; i < entry.Size; i++ {
			_, err := file.Write(filler)
			must(t, err)
		}
		must(t, file.Close())
	}

	if testSymlinks {
		for _, entry := range symlinks {
			new := filepath.Join(tmpPath, entry.Newname)
			must(t, os.Symlink(entry.Oldname, new))
		}
	}

	return tmpPath
}

func compressZip(archiveWriter io.Writer, dir string, consumer *state.Consumer) error {
	var err error

	zipWriter := zip.NewWriter(archiveWriter)
	defer zipWriter.Close()
	defer func() {
		if zipWriter != nil {
			if zErr := zipWriter.Close(); err == nil && zErr != nil {
				err = errors.WithStack(zErr)
			}
		}
	}()

	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		name, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		if name == "." {
			// don't add '.' to zip
			return nil
		}

		name = filepath.ToSlash(name)

		fh, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}

		fh.Name = name

		writer, err := zipWriter.CreateHeader(fh)
		if err != nil {
			return err
		}

		if info.IsDir() {
			// good!
		} else if info.Mode()&os.ModeSymlink > 0 {
			dest, err := os.Readlink(path)
			if err != nil {
				return err
			}

			_, err = writer.Write([]byte(dest))
			if err != nil {
				return err
			}
		} else if info.Mode().IsRegular() {
			reader, err := os.Open(path)
			if err != nil {
				return err
			}
			defer reader.Close()

			_, err = io.Copy(writer, reader)
			if err != nil {
				return err
			}
		}

		return nil
	})

	err = zipWriter.Close()
	if err != nil {
		return errors.WithStack(err)
	}
	zipWriter = nil
	return nil
}
