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
	"path/filepath"
	"strings"
	"sync"

	"github.com/cespare/grb/internal/grb"
)

const (
	cacheDir    = "cache"
	gopathDir   = "gopath"
	hashSize    = sha256.Size * 2 // it's hex
	buildIDSize = 16 * 2          // also hex
)

type Server struct {
	DataDir string

	mu sync.Mutex
}

func NewServer(dataDir string) (*Server, error) {
	for _, dir := range []string{dataDir, cacheDir} {
		if err := os.MkdirAll(filepath.Join(dataDir, dir), 0755); err != nil {
			return nil, err
		}
	}
	return &Server{DataDir: dataDir}, nil
}

func (s *Server) HandleBegin(w http.ResponseWriter, r *http.Request) {
	id := newBuildID()
	br := &grb.BuildResponse{
		ID: id,
	}
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(br); err != nil {
		log.Println("/begin error:", err)
		http.Error(w, "bwah?", 500)
		return
	}
}

func (s *Server) HandleUpload(w http.ResponseWriter, r *http.Request, hash []byte) {
}

func (s *Server) HandleCompile(w http.ResponseWriter, buildID []byte) {
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
		s.HandleUpload(w, r, []byte(rest))
		return
	}
	if rest, ok := trimPrefix(r.URL.Path, "/compile/"); ok {
		if r.Method != "GET" {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		s.HandleCompile(w, []byte(rest))
		return
	}
	http.Error(w, "not found", 404)
}

// trimPrefix is like strings.TrimPrefix
// but it also returns whether such a prefix was found.
func trimPrefix(s, prefix string) (string, bool) {
	s2 := strings.TrimPrefix(s, prefix)
	return s2, s != s2
}

func newBuildID() string {
	s := make([]byte, buildIDSize/2)
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
	)
	flag.Parse()

	server, err := NewServer(*dataDir)
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{
		Addr:    *addr,
		Handler: server,
	}
	log.Println("Now listening on", *addr)
	log.Fatal(srv.ListenAndServe())
}
