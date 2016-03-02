package grb

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

var errHashMismatch = errors.New("SHA256 hash of uploaded file doesn't match declared hash")

type Cache string

func (c Cache) Path(hash string) string {
	return filepath.Join(string(c), hash[:2], hash[2:])
}

func (c Cache) Put(hash string, r io.Reader) error {
	f, err := ioutil.TempFile(string(c), "grbcache")
	if err != nil {
		return err
	}
	defer func() {
		// only needed in error cases
		f.Close()
		os.Remove(f.Name())
	}()

	h := sha256.New()
	tr := io.TeeReader(r, h)
	if _, err := io.Copy(f, tr); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	if hex.EncodeToString(h.Sum(nil)[:]) != hash {
		return errHashMismatch
	}

	dest := c.Path(hash)
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	// TODO: a renameat2 with RENAME_NOREPLACE would be conceptually nicer.
	return os.Rename(f.Name(), dest)
}

func (c Cache) FindMissing(packages []*Package) ([]*Package, error) {
	var missing []*Package
	for _, pkg := range packages {
		var files []File
		for _, file := range pkg.Files {
			_, err := os.Stat(c.Path(file.Hash))
			if err != nil {
				if !os.IsNotExist(err) {
					return nil, err
				}
				files = append(files, file)
			}
		}
		if len(files) > 0 {
			missing = append(missing, &Package{
				Name:  pkg.Name,
				Files: files,
			})
		}
	}
	return missing, nil
}
