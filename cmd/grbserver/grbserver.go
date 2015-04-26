package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cespare/grb/internal/github.com/cespare/hutil/apachelog"
	"github.com/cespare/grb/internal/grb"
)

const (
	cacheDir    = "cache"
	gopathDir   = "gopath"
	hashSize    = sha256.Size * 2 // it's hex
	buildIDSize = 16 * 2          // also hex
	timeout     = 5 * time.Minute
)

type Server struct {
	DataDir string
	Goroot  string
	Cache   Cache

	mu     sync.Mutex
	builds map[string]*grb.BuildRequest
}

func NewServer(dataDir, goroot string) (*Server, error) {
	for _, dir := range []string{gopathDir, cacheDir} {
		if err := os.MkdirAll(filepath.Join(dataDir, dir), 0755); err != nil {
			return nil, err
		}
	}
	return &Server{
		DataDir: dataDir,
		Goroot:  goroot,
		Cache:   Cache(filepath.Join(dataDir, cacheDir)),
		builds:  make(map[string]*grb.BuildRequest),
	}, nil
}

func (s *Server) HandleBegin(w http.ResponseWriter, r *http.Request) {
	var breq grb.BuildRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&breq); err != nil {
		http.Error(w, "malformed BuildRequest: "+err.Error(), http.StatusBadRequest)
		return
	}

	id := randomString(buildIDSize / 2)
	s.mu.Lock()
	s.builds[id] = &breq
	s.mu.Unlock()
	time.AfterFunc(timeout, func() {
		s.mu.Lock()
		delete(s.builds, id)
		s.mu.Unlock()
	})

	missing, err := s.Cache.FindMissing(breq.Packages)
	if err != nil {
		log.Println("/begin error:", err)
		http.Error(w, "womp womp", 500)
		return
	}
	br := &grb.BuildResponse{
		ID:      id,
		Missing: missing,
	}
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(br); err != nil {
		log.Println("/begin error:", err)
		http.Error(w, "bwah?", 500)
		return
	}
}

func (s *Server) HandleUpload(w http.ResponseWriter, r *http.Request, hash string) {
	if len(hash) != hashSize {
		http.Error(w, "bad hash size", http.StatusBadRequest)
		return
	}
	if err := s.Cache.Put(hash, r.Body); err != nil {
		// TODO: better error here
		http.Error(w, "error inserting into file cache: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Server) HandleBuild(w http.ResponseWriter, buildID string) {
	if len(buildID) != buildIDSize {
		http.Error(w, "bad build id", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	breq, ok := s.builds[buildID]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "no such build", http.StatusBadRequest)
		return
	}
	s.Build(w, buildID, breq)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/begin" {
		if r.Method != "POST" {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		s.HandleBegin(w, r)
		return
	}
	if rest, ok := trimPrefix(r.URL.Path, "/upload/"); ok {
		if r.Method != "POST" {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		s.HandleUpload(w, r, rest)
		return
	}
	if rest, ok := trimPrefix(r.URL.Path, "/build/"); ok {
		if r.Method != "GET" {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		s.HandleBuild(w, rest)
		return
	}
	if r.URL.Path == "/version" {
		if r.Method != "GET" {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		s.HandleVersion(w)
		return
	}
	http.Error(w, "not found", 404)
}

func (s *Server) HandleVersion(w http.ResponseWriter) {
	cmd := s.goCmd("version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Println("Error calling 'go version':", err)
		os.Stderr.Write(out)
		http.Error(w, "error getting Go version", http.StatusInternalServerError)
		return
	}
	w.Write(out)
}

func (s *Server) goCmd(args ...string) *exec.Cmd {
	bin := "go"
	if s.Goroot != "" {
		bin = filepath.Join(s.Goroot, "bin", "go")
	}
	cmd := exec.Command(bin, args...)
	if s.Goroot != "" {
		cmd.Env = []string{"GOROOT=" + s.Goroot}
	}
	return cmd
}

// trimPrefix is like strings.TrimPrefix
// but it also returns whether such a prefix was found.
func trimPrefix(s, prefix string) (string, bool) {
	s2 := strings.TrimPrefix(s, prefix)
	return s2, s != s2
}

func randomString(n int) string {
	s := make([]byte, n)
	_, err := rand.Read(s)
	if err != nil {
		panic(err)
	}
	return hex.EncodeToString(s)
}

func main() {
	var (
		dataDir = flag.String("datadir", "", "data directory")
		addr    = flag.String("addr", "localhost:6363", "listen addr")
		goroot  = flag.String("goroot", "", "explicitly set Go directory")
		tls     = flag.Bool("tls", false, "serve HTTPS traffic (-tlscert and -tlskey must be provided)")
		tlsCert = flag.String("tlscert", "", "cert.pem for TLS")
		tlsKey  = flag.String("tlskey", "", "cert.key for TLS")
	)
	flag.Parse()

	server, err := NewServer(*dataDir, *goroot)
	if err != nil {
		log.Fatal(err)
	}
	if *tls && (*tlsCert == "" || *tlsKey == "") {
		log.Fatal("If -tls is given, -tlscert and -tlskey must also be provided")
	}

	srv := &http.Server{
		Addr:    *addr,
		Handler: apachelog.NewDefaultHandler(server),
	}
	log.Println("Now listening on", *addr)
	if *tls {
		log.Fatal(srv.ListenAndServeTLS(*tlsCert, *tlsKey))
	}
	log.Fatal(srv.ListenAndServe())
}
