package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"go/build"
	"os"

	"github.com/davecgh/go-spew/spew"
)

type File struct {
	Name   string
	SHA256 [sha256.Size]byte
}

type Package struct {
	Name  string
	Files []File
}

func NewPackage(pkg *build.Package) *Package {

}

func findPackages(pkgName string, alreadyFound map[string]struct{}) ([]*Package, error) {
	pkg, err := build.Import(pkgName, "/relative/imports/not/allowed", 0)
	if err != nil {
		return nil, err
	}
	var packages []*Package
	for _, pkgName2 := range pkg.Imports {
		if _, ok := alreadyFound[pkgName2]; ok {
			continue
		}
		alreadyFound[pkgName2] = struct{}{}
		packages2, err := findPackages(pkgName2, alreadyFound)
		if err != nil {
			return nil, err
		}
		packages = append(packages, packages2...)
	}
	packages := append(packages, NewPackage(pkg))
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
	fmt.Printf("\033[01;32m>>>> pkg:\n%s<<<<\x1B[m\n", spew.Sdump(pkg))
}
