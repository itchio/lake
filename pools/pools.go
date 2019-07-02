package pools

import (
	"strings"

	"github.com/itchio/arkive/zip"

	"path/filepath"

	"github.com/itchio/httpkit/eos"
	"github.com/itchio/lake"
	"github.com/itchio/lake/pools/fspool"
	"github.com/itchio/lake/pools/zippool"
	"github.com/itchio/lake/tlc"
	"github.com/pkg/errors"
)

func New(c *tlc.Container, basePath string) (lake.Pool, error) {
	if basePath == "/dev/null" {
		return fspool.New(c, basePath), nil
	}

	fr, err := eos.Open(basePath)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	targetInfo, err := fr.Stat()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if targetInfo.IsDir() {
		err := fr.Close()
		if err != nil {
			return nil, err
		}

		return fspool.New(c, basePath), nil
	}

	if strings.HasSuffix(strings.ToLower(targetInfo.Name()), ".zip") {
		zr, err := zip.NewReader(fr, targetInfo.Size())
		if err != nil {
			return nil, errors.WithStack(err)
		}
		return zippool.New(c, zr), nil
	}

	// assume single-file container
	fsp := fspool.New(c, filepath.Dir(basePath))
	fsp.UniqueReader = fr
	return fsp, nil
}
