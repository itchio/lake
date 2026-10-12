package tlc

import (
	"os"

	"github.com/pkg/errors"
)

// Prepare creates all directories, files, and symlinks.
// It also applies the proper permissions if the files already exist
func (c *Container) Prepare(basePath string) error {
	err := os.MkdirAll(basePath, 0o755)
	if err != nil {
		return errors.WithStack(err)
	}

	for _, dirEntry := range c.Dirs {
		err := c.prepareDir(basePath, dirEntry)
		if err != nil {
			return errors.WithStack(err)
		}
	}

	for _, fileEntry := range c.Files {
		err := c.prepareFile(basePath, fileEntry)
		if err != nil {
			return errors.WithStack(err)
		}
	}

	for _, link := range c.Symlinks {
		err := c.prepareSymlink(basePath, link)
		if err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func (c *Container) prepareDir(basePath string, dirEntry *Dir) error {
	fullPath, err := ContainedPath(basePath, dirEntry.Path)
	if err != nil {
		return errors.WithStack(err)
	}
	// MkdirAll is a no-op on a symlink to a directory and Chmod follows it.
	if stat, err := os.Lstat(fullPath); err == nil && stat.Mode()&os.ModeSymlink != 0 {
		err = os.Remove(fullPath)
		if err != nil {
			return errors.WithStack(err)
		}
	}
	err = os.MkdirAll(fullPath, os.FileMode(dirEntry.Mode))
	if err != nil {
		return errors.WithStack(err)
	}
	err = os.Chmod(fullPath, os.FileMode(dirEntry.Mode))
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func (c *Container) prepareFile(basePath string, fileEntry *File) error {
	fullPath, err := ContainedPath(basePath, fileEntry.Path)
	if err != nil {
		return errors.WithStack(err)
	}
	// A symlink at the file's path is foreign; opening would follow it.
	if stat, err := os.Lstat(fullPath); err == nil && stat.Mode()&os.ModeSymlink != 0 {
		err = os.Remove(fullPath)
		if err != nil {
			return errors.WithStack(err)
		}
	}
	file, err := os.OpenFile(fullPath, os.O_CREATE, os.FileMode(fileEntry.Mode))
	if err != nil {
		return errors.WithStack(err)
	}
	err = file.Close()
	if err != nil {
		return errors.WithStack(err)
	}

	// we explicitly don't want to open with O_TRUNC, because we might
	// be resuming a patching operation. however, if the file is too large
	// we want to make it smaller.
	// note that this does not work for preallocation.
	err = os.Truncate(fullPath, fileEntry.Size)
	if err != nil {
		return errors.WithStack(err)
	}

	err = os.Chmod(fullPath, os.FileMode(fileEntry.Mode))
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func (c *Container) prepareSymlink(basePath string, link *Symlink) error {
	fullPath, err := ContainedPath(basePath, link.Path)
	if err != nil {
		return errors.WithStack(err)
	}
	err = ContainedSymlinkTarget(basePath, fullPath, link.Dest)
	if err != nil {
		return errors.WithStack(err)
	}
	err = os.RemoveAll(fullPath)
	if err != nil {
		return errors.WithStack(err)
	}

	err = os.Symlink(link.Dest, fullPath)
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}
