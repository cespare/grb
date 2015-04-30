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
	"path/filepath"
	"strings"
	"time"

	"github.com/cespare/grb/internal/grb"
	"github.com/davecgh/go-spew/spew"
)

const (
	timeout     = 10 * time.Second
	parallelism = 10
)

func findPackages(path string, alreadyFound map[string]struct{}) ([]*grb.Package, error) {
	pkg, err := build.Import(path, ".", 0)
	if err != nil {
		return nil, err
	}
	if pkg.Goroot {
		// ignore stdlib
		return nil, nil
	}
	var pkgs []*grb.Package
	for _, depPkgName := range pkg.Imports {
		if _, ok := alreadyFound[depPkgName]; ok {
			continue
		}
		alreadyFound[depPkgName] = struct{}{}
		depPkg, err := findPackages(depPkgName, alreadyFound)
		if err != nil {
			return nil, err
		}
		pkgs = append(pkgs, depPkg...)
	}
	p, err := grb.NewPackage(pkg)
	if err != nil {
		return nil, err
	}
	pkgs = append(pkgs, p)
	return pkgs, nil
}

var (
	errStatusNot200 = errors.New("non-200 status from build server")
)

type BuildConfig struct {
	PkgPath    string
	ServerURL  string
	OutputName string
	Race       bool
}

func runBuild(conf *BuildConfig) error {
	l.Println("Finding dependencies of", conf.PkgPath)
	pkgs, err := findPackages(conf.PkgPath, make(map[string]struct{}))
	if err != nil {
		return err
	}
	fmt.Printf("\033[01;32m>>>> pkgs:\n%s<<<<\x1B[m\n", spew.Sdump(pkgs))
	return nil
	l.Printf("Found %d packages for build", len(pkgs))
	client := newHTTPClient()

	breq := &grb.BuildRequest{
		PackageName: conf.PkgPath,
		Packages:    pkgs,
		Race:        conf.Race,
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	if err := encoder.Encode(breq); err != nil {
		return err
	}

	// Step 1: POST /begin to kick off the build.
	// The response says which files the server doesn't know about.

	url := conf.ServerURL + "/begin"
	l.Println("POST", url)
	resp, err := client.Post(url, "application/json", &buf)
	if err != nil {
		l.Println("Error making POST request:", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		l.Println("Non-200 status code from /begin:", resp.StatusCode)
		return errStatusNot200
	}

	var bresp grb.BuildResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&bresp); err != nil {
		l.Println("JSON decoding error with result of /begin:", err)
		return err
	}

	// Step 2: POST /upload to send all the missing files to the server.

	var nFiles int
	l.Printf("Starting upload of missing files in %d packages", len(bresp.Missing))
	for _, pkg := range bresp.Missing {
		for _, file := range pkg.Files {
			nFiles++
			l.Printf("Uploading file %s from package %s (%s)", file.Name, pkg.Name, file.LocalPath)
			if err := uploadFile(&file, conf.ServerURL, client); err != nil {
				return err
			}
		}
	}
	l.Printf("Successfully uploaded %d files from %d packages", nFiles, len(bresp.Missing))

	// Step 3: GET /build to build and download the result.

	url = conf.ServerURL + "/build/" + bresp.ID
	l.Println("GET", url)
	resp, err = client.Get(url)
	if err != nil {
		l.Println("Error making GET request:", err)
		return err
	}
	if resp.StatusCode == 412 {
		log.Println("Build error:")
		io.Copy(os.Stderr, resp.Body)
	}
	if resp.StatusCode != 200 {
		l.Println("Non-200 status code from /begin:", resp.StatusCode)
		return errStatusNot200
	}
	f, err := os.Create(conf.OutputName)
	if err != nil {
		return err
	}
	l.Println("200 result for GET request; downloading/writing result")
	if _, err := io.Copy(f, resp.Body); err != nil {
		l.Println("Error downloading file to disk:", err)
		f.Close()
		os.Remove(conf.OutputName)
		return err
	}
	if err := f.Close(); err != nil {
		l.Println("Error writing/closing output file:", err)
		return err
	}
	if err := os.Chmod(conf.OutputName, 0755); err != nil {
		l.Println("Chmod error with output artifact:", err)
		return err
	}
	l.Println("Build complete")
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

var l *log.Logger

func main() {
	var (
		out     = flag.String("o", "", "specify output file name")
		race    = flag.Bool("race", false, "build with -race flag")
		verbose = flag.Bool("v", false, "show logging messages")
	)
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `usage: grb [flags] [package]

where the flags are:
`)
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 1 {
		flag.Usage()
	}
	var logOutput io.Writer = os.Stderr
	if !*verbose {
		logOutput = ioutil.Discard
	}
	l = log.New(logOutput, "", log.Lmicroseconds)

	serverURL := os.Getenv("GRB_SERVER_URL")
	if serverURL == "" {
		log.Fatal("Must provide environment variable GRB_SERVER_URL.")
	}

	path := "."
	if flag.NArg() == 1 {
		path = flag.Arg(0)
	}
	var outputName string
	switch {
	case *out != "":
		outputName = *out
	case strings.HasPrefix(path, "."):
		abs, err := filepath.Abs(path)
		if err != nil {
			log.Fatal("Cannot resolve path %q: %s", path, err)
		}
		outputName = filepath.Base(abs)
	default:
		parts := strings.Split(path, "/")
		outputName = parts[len(parts)-1]
	}

	fmt.Printf("\033[01;34m>>>> path: %v\x1B[m\n", path)
	fmt.Printf("\033[01;34m>>>> outputName: %v\x1B[m\n", outputName)

	conf := &BuildConfig{
		PkgPath:    path,
		ServerURL:  serverURL,
		OutputName: outputName,
		Race:       *race,
	}
	if err := runBuild(conf); err != nil {
		log.Fatal("Fatal error:", err)
	}
}
