package grb

import (
	"crypto/sha256"
	"encoding/hex"
	"go/build"
	"io"
	"os"
	"path/filepath"
)

type File struct {
	Name string
	Hash string
}

type Package struct {
	Name  string
	Files []File
}

func NewPackage(pkg *build.Package) (*Package, error) {
	var files []File
	for _, fs := range [][]string{
		pkg.GoFiles, pkg.CgoFiles, pkg.CFiles, pkg.CXXFiles,
		pkg.MFiles, pkg.HFiles, pkg.SFiles, pkg.SwigFiles,
		pkg.SwigCXXFiles, pkg.SysoFiles,
	} {
		for _, filename := range fs {
			path := filepath.Join(pkg.Dir, filename)
			hash, err := hashFile(path)
			if err != nil {
				return nil, err
			}
			files = append(files, File{
				Name: filename,
				Hash: hash,
			})
		}
	}
	return &Package{
		Name:  pkg.ImportPath,
		Files: files,
	}, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)[:]), nil
}

type BuildRequest struct {
	PackageName string
	Packages    []*Package
}

type BuildResponse struct {
	ID      string
	Missing []*Package
}
