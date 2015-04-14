package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"go/build"
	"io"
	"log"
	"os"
	"path/filepath"
)

type File struct {
	Name string
	Hash []byte
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

func hashFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return nil, err
	}
	return hash.Sum(nil)[:], nil
}

func findPackages(pkgName string, alreadyFound map[string]struct{}) ([]*Package, error) {
	pkg, err := build.Import(pkgName, "/relative/imports/not/allowed", 0)
	if err != nil {
		return nil, err
	}
	if pkg.Goroot {
		// ignore stdlib
		return nil, nil
	}
	var packages []*Package
	for _, depPkgName := range pkg.Imports {
		if _, ok := alreadyFound[depPkgName]; ok {
			continue
		}
		alreadyFound[depPkgName] = struct{}{}
		depPkg, err := findPackages(depPkgName, alreadyFound)
		if err != nil {
			return nil, err
		}
		packages = append(packages, depPkg...)
	}
	p, err := NewPackage(pkg)
	if err != nil {
		return nil, err
	}
	packages = append(packages, p)
	return packages, nil
}

func main() {
	out := flag.String("o", "", "specify output file name")
	race := flag.Bool("race", false, "build with -race flag")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `usage: grb [flags] [package]

where the flags are:
`)
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
	}
	_ = *out
	if *race {
		panic("unimplemented")
	}

	pkgName := flag.Arg(0)
	pkgs, err := findPackages(pkgName, make(map[string]struct{}))
	if err != nil {
		log.Fatal(err)
	}
	for _, pkg := range pkgs {
		fmt.Printf("\033[01;34m>>>> pkg: %v\x1B[m\n", pkg)
	}
}
