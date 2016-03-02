package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/build"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cespare/grb/internal/grb"
)

const (
	timeout     = 10 * time.Second
	parallelism = 10
)

func FindPackages(pkgName string, env *Env, gopath string) ([]*grb.Package, error) {
	ctx := build.Default
	if gopath != "" {
		ctx.GOPATH = gopath
	}
	ctx.GOOS = env.GOOS
	ctx.GOARCH = env.GOARCH
	pkg, err := ctx.Import(pkgName, "/relative/imports/not/allowed", build.FindOnly)
	if err != nil {
		return nil, err
	}
	return findPackages(pkgName, pkg.Dir, &ctx, make(map[string]struct{}))
}

func findPackages(pkgName, srcDir string, ctx *build.Context, alreadyFound map[string]struct{}) ([]*grb.Package, error) {
	if pkgName == "C" {
		return nil, nil
	}
	pkg, err := ctx.Import(pkgName, srcDir, 0)
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
		depPkg, err := findPackages(depPkgName, pkg.Dir, ctx, alreadyFound)
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

var (
	errStatusNot200 = errors.New("non-200 status from build server")
)

type BuildConfig struct {
	PkgName    string
	ServerURL  string
	OutputName string
	Flags      []string
	GOPATH     string
}

type Env struct {
	GOOS    string
	GOARCH  string
	Version string
}

func runBuild(conf *BuildConfig) error {
	// Step 1: Get server environment info so we know what files to send,
	// then determine all dependencies and their files.

	url := conf.ServerURL + "/version?format=json"
	client := newHTTPClient()
	log.Println("GET", url)
	resp, err := client.Get(url)
	if err != nil {
		log.Println("Error making GET request:", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Println("Non-200 status code from version:", resp.StatusCode)
		return errStatusNot200
	}
	var env Env
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		log.Println("Could not decode /version JSON:", err)
		return err
	}
	log.Printf("Remote server has environment %+v", env)

	log.Println("Finding dependencies of", conf.PkgName)
	pkgs, err := FindPackages(conf.PkgName, &env, conf.GOPATH)
	if err != nil {
		return err
	}
	log.Printf("Found %d packages for build", len(pkgs))

	breq := &grb.BuildRequest{
		PackageName: conf.PkgName,
		Packages:    pkgs,
		Flags:       conf.Flags,
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	if err := encoder.Encode(breq); err != nil {
		return err
	}

	// Step 2: POST /begin to kick off the build.
	// The response says which files the server doesn't know about.

	url = conf.ServerURL + "/begin"
	log.Println("POST", url)
	resp, err = client.Post(url, "application/json", &buf)
	if err != nil {
		log.Println("Error making POST request:", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Println("Non-200 status code from /begin:", resp.StatusCode)
		return errStatusNot200
	}

	var bresp grb.BuildResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&bresp); err != nil {
		log.Println("JSON decoding error with result of /begin:", err)
		return err
	}

	// Step 3: POST /upload to send all the missing files to the server.

	var nFiles int
	log.Printf("Starting upload of missing files in %d packages", len(bresp.Missing))
	for _, pkg := range bresp.Missing {
		for _, file := range pkg.Files {
			nFiles++
			log.Printf("Uploading file %s from package %s (%s)", file.Name, pkg.Name, file.LocalPath)
			if err := uploadFile(&file, conf.ServerURL, client); err != nil {
				return err
			}
		}
	}
	log.Printf("Successfully uploaded %d files from %d packages", nFiles, len(bresp.Missing))

	// Step 4: GET /build to build and download the result.

	url = conf.ServerURL + "/build/" + bresp.ID
	log.Println("GET", url)
	resp, err = client.Get(url)
	if err != nil {
		log.Println("Error making GET request:", err)
		return err
	}
	if resp.StatusCode == 412 {
		log.Println("Build error:")
		io.Copy(os.Stderr, resp.Body)
	}
	if resp.StatusCode != 200 {
		log.Println("Non-200 status code from /begin:", resp.StatusCode)
		return errStatusNot200
	}
	f, err := os.Create(conf.OutputName)
	if err != nil {
		return err
	}
	log.Println("200 result for GET request; downloading/writing result")
	if _, err := io.Copy(f, resp.Body); err != nil {
		log.Println("Error downloading file to disk:", err)
		f.Close()
		os.Remove(conf.OutputName)
		return err
	}
	if err := f.Close(); err != nil {
		log.Println("Error writing/closing output file:", err)
		return err
	}
	if err := os.Chmod(conf.OutputName, 0755); err != nil {
		log.Println("Chmod error with output artifact:", err)
		return err
	}
	log.Println("Build complete")
	return nil
}

func uploadFile(file *grb.File, serverURL string, client *http.Client) error {
	f, err := os.Open(file.LocalPath)
	if err != nil {
		return err
	}
	defer f.Close()
	url := serverURL + "/upload/" + file.Hash
	resp, err := client.Post(url, "application/octet-stream", f)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		io.Copy(os.Stdout, resp.Body)
		return errStatusNot200
	}
	return nil
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout:   timeout,
				KeepAlive: timeout,
			}).Dial,
			TLSHandshakeTimeout: timeout,
			MaxIdleConnsPerHost: parallelism,
		},
	}
}

type grbConfig struct {
	serverURL string
	verbose   bool
	out       string
	race      bool
	ldflags   string
	pkg       string
	gopath    string
}

func runGRB(c grbConfig) error {
	if !c.verbose {
		log.SetOutput(ioutil.Discard)
		defer log.SetOutput(os.Stderr)
	}
	pkgParts := strings.Split(c.pkg, "/")
	outputName := pkgParts[len(pkgParts)-1]
	if c.out != "" {
		outputName = c.out
	}
	var flags []string
	if c.race {
		flags = append(flags, "-race")
	}
	if c.ldflags != "" {
		flags = append(flags, "-ldflags", c.ldflags)
	}
	conf := &BuildConfig{
		PkgName:    c.pkg,
		ServerURL:  c.serverURL,
		OutputName: outputName,
		Flags:      flags,
		GOPATH:     c.gopath,
	}
	return runBuild(conf)
}

func main() {
	var c grbConfig
	flag.StringVar(&c.out, "o", "", "specify output file name")
	flag.BoolVar(&c.race, "race", false, "build with -race flag")
	flag.StringVar(&c.ldflags, "ldflags", "", "build with -ldflags flag")
	flag.BoolVar(&c.verbose, "v", false, "show logging messages")
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
	log.SetFlags(log.Lmicroseconds)
	serverURL := os.Getenv("GRB_SERVER_URL")
	if serverURL == "" {
		log.Fatal("Must provide environment variable GRB_SERVER_URL.")
	}
	if err := runGRB(c); err != nil {
		log.Fatal("Fatal error:", err)
	}
}
