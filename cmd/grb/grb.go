package main

import (
	"flag"
	"fmt"
	"go/build"
	"log"
	"os"

	"github.com/cespare/grb/internal/grb"
)

func findPackages(pkgName string, alreadyFound map[string]struct{}) ([]*grb.Package, error) {
	pkg, err := build.Import(pkgName, "/relative/imports/not/allowed", 0)
	if err != nil {
		return nil, err
	}
	if pkg.Goroot {
		// ignore stdlib
		return nil, nil
	}
	var packages []*grb.Package
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
	p, err := grb.NewPackage(pkg)
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
