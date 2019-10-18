package tlc

import "github.com/mitchellh/copystructure"

type Entry interface {
	GetPath() string
	SetPath(path string)

	GetMode() uint32
	SetMode(mode uint32)
}

var _ Entry = (*File)(nil)
var _ Entry = (*Dir)(nil)
var _ Entry = (*Symlink)(nil)

//--------- File

func (f *File) GetPath() string {
	return f.Path
}

func (f *File) SetPath(path string) {
	f.Path = path
}

func (f *File) GetMode() uint32 {
	return f.Mode
}

func (f *File) SetMode(mode uint32) {
	f.Mode = mode
}

//--------- Symlink

func (s *Symlink) GetPath() string {
	return s.Path
}

func (s *Symlink) SetPath(path string) {
	s.Path = path
}

func (s *Symlink) GetMode() uint32 {
	return s.Mode
}

func (s *Symlink) SetMode(mode uint32) {
	s.Mode = mode
}

//--------- Dir

func (d *Dir) GetPath() string {
	return d.Path
}

func (d *Dir) SetPath(path string) {
	d.Path = path
}

func (d *Dir) GetMode() uint32 {
	return d.Mode
}

func (d *Dir) SetMode(mode uint32) {
	d.Mode = mode
}

//--------- Dir

type ForEachOutcome int

const (
	ForEachContinue = 1
	ForEachBreak    = 2
)

// ForEachEntry iterates through all entries of a container
// Return `true` to break
func (c *Container) ForEachEntry(f func(e Entry) ForEachOutcome) {
	for _, e := range c.Dirs {
		if f(e) == ForEachBreak {
			return
		}
	}
	for _, e := range c.Files {
		if f(e) == ForEachBreak {
			return
		}
	}
	for _, e := range c.Symlinks {
		if f(e) == ForEachBreak {
			return
		}
	}
}

// Clone returns a deep clone of this container
func (c *Container) Clone() *Container {
	interf, err := copystructure.Copy(c)
	if err != nil {
		panic(err)
	}

	c2, ok := interf.(*Container)
	if !ok {
		panic("copystructure.Copy(*tlc.Container) did not return a *tlc.Container")
	}

	return c2
}
