# lake

[![GoDoc](https://godoc.org/github.com/itchio/lake?status.svg)](https://godoc.org/github.com/itchio/lake)
[![MIT licensed](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/itchio/lake/blob/master/LICENSE)

Lake is a Go library that provides a unified interface for reading and writing
collections of files across different storage backends: filesystem directories,
ZIP archives, and single files. It is part of the [itch.io](https://itch.io)
ecosystem and primarily used by [wharf](https://github.com/itchio/wharf).

## Core concepts

### Pool

A **Pool** gives indexed read/seek access to an ordered list of files. Pools
are backed by different storage types but expose the same interface:

```go
type Pool interface {
    GetSize(fileIndex int64) int64
    GetReader(fileIndex int64) (io.Reader, error)
    GetReadSeeker(fileIndex int64) (io.ReadSeeker, error)
    Close() error
}
```

**WritablePool** extends Pool with write access, and **CaseFixerPool** handles
renaming on-disk files to match a container's expected casing (useful for
case-insensitive filesystems).

### TLC Container

The `tlc.Container` is a protobuf-based metadata format that captures a
complete directory structure: files, directories, and symlinks with their
sizes, permissions, and paths. Containers are produced by walking a directory or
ZIP archive, and can be used to recreate that structure on disk.

[wharf](https://github.com/itchio/wharf) embeds `Container` structs serialized
via protobuf in patch files (`.pwr`) and signature files to describe both the "old" and
"new" versions of a build, paired with binary diffs of the actual file
contents. [butler](https://github.com/itchio/butler) is the CLI that drives
wharf to produce and apply these patches for itch.io uploads.

Lake does not define the patch format itself, it provides the filesystem
abstraction layer and directory manifest that wharf builds on top of.

## Package structure

### `lake` (root)

Core interfaces: `Pool`, `WritablePool`, `CaseFixerPool`, and case-fix
helpers.

### `tlc/`

Container type and filesystem operations:

- **Walking**: `WalkDir`, `WalkZip`, `WalkAny` (auto-detects), `WalkSingle`
- **Preparing**: `Container.Prepare` creates all directories, files, and symlinks from a container spec
- **Validation**: Container integrity checks
- **Comparison**: Diff containers to detect changes
- **Permissions**: Executable detection (ELF, Mach-O, shell scripts)
- **Case handling**: Cross-platform case-insensitive filesystem support
- **Filtering**: `FilterFunc` and built-in `PresetFilter` to ignore VCS metadata, OS junk files, etc.

### `pools/`

Pool implementations for different backends:

| Package | Description |
|---------|-------------|
| `fspool` | Filesystem-backed pool (reads/writes files in a directory) |
| `zippool` | Read-only pool backed by a ZIP archive |
| `zipwriterpool` | Write-only pool that produces a ZIP archive |
| `cachepool` | Caching wrapper around another pool |
| `nullpool` | Discards all data (useful for testing and benchmarking) |
| `singlefilepool` | Writes a single file |

## Usage

```go
import (
    "github.com/itchio/lake/tlc"
    "github.com/itchio/lake/pools/fspool"
)

// Walk a directory to get its container (file listing)
container, err := tlc.WalkDir("/path/to/dir", tlc.WalkOpts{
    Filter: tlc.PresetFilter,
})

// Open a filesystem pool for reading
pool := fspool.New(container, "/path/to/dir")
defer pool.Close()

reader, err := pool.GetReader(0) // read first file
```

## License

MIT
