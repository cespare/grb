package main

import (
	"io/ioutil"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cespare/grb/internal/grb"
)

type testGRB struct {
	t      *testing.T
	tmp    string
	server *httptest.Server
}

func newTestGRB(t *testing.T) *testGRB {
	tmp, err := ioutil.TempDir(".", "test-end-to-end-")
	if err != nil {
		t.Fatal(err)
	}
	server, err := grb.NewServer(filepath.Join(tmp, "data"), "")
	if err != nil {
		t.Fatal(err)
	}
	return &testGRB{
		t:      t,
		tmp:    tmp,
		server: httptest.NewServer(server),
	}
}

func (tg *testGRB) cleanup() {
	tg.server.Close()
	os.RemoveAll(tg.tmp)
}

func (tg *testGRB) build(pkg, bin string) {
	c := grbConfig{
		serverURL: tg.server.URL,
		out:       bin,
		pkg:       pkg,
		gopath:    "testdata",
	}
	if err := runGRB(c); err != nil {
		tg.t.Fatalf("Error running grb: %s", err)
	}
}

func (tg *testGRB) run(bin string) string {
	out, err := exec.Command(bin).Output()
	if err != nil {
		tg.t.Fatalf("Error running test program: %s", err)
	}
	return strings.TrimSpace(string(out))
}

func TestHello(t *testing.T) {
	tg := newTestGRB(t)
	defer tg.cleanup()

	bin := filepath.Join(tg.tmp, "hello")
	tg.build("hello", bin)
	got := tg.run(bin)
	if want := "a"; got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
}

func TestVendor(t *testing.T) {
	tg := newTestGRB(t)
	defer tg.cleanup()

	bin := filepath.Join(tg.tmp, "v")
	tg.build("v", bin)
	got := tg.run(bin)
	if want := "a vendored"; got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
}
