package grb

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
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
	builds map[string]*BuildRequest
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
		builds:  make(map[string]*BuildRequest),
	}, nil
}

func (s *Server) HandleBegin(w http.ResponseWriter, r *http.Request) {
	var breq BuildRequest
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
	br := &BuildResponse{
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
		if r.URL.Query().Get("format") == "json" {
			s.HandleVersionJSON(w)
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

func (s *Server) HandleVersionJSON(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"GOOS":%q,"GOARCH":%q,"Version":%q}`,
		runtime.GOOS, runtime.GOARCH, runtime.Version())
}

func (s *Server) goCmd(args ...string) *exec.Cmd {
	bin := "go"
	if s.Goroot != "" {
		bin = filepath.Join(s.Goroot, "bin", "go")
	}
	cmd := exec.Command(bin, args...)
	if s.Goroot != "" {
		cmd.Env = append(os.Environ(), "GOROOT="+s.Goroot)
	}
	return cmd
}

func (s *Server) Build(w http.ResponseWriter, buildID string, breq *BuildRequest) {
	root := filepath.Join(s.DataDir, gopathDir, buildID+"."+randomString(4))
	defer os.RemoveAll(root)

	if err := s.buildGOPATH(breq, root); err != nil {
		log.Println("Error building GOPATH:", err)
		http.Error(w, "error creating build", http.StatusInternalServerError)
		return
	}
	args := []string{"build", "-o", buildID}
	args = append(args, breq.Flags...)
	args = append(args, breq.PackageName)
	cmd := s.goCmd(args...)
	cmd.Dir = root
	gopath, err := filepath.Abs(root)
	if err != nil {
		log.Println("Error building GOPATH:", err)
		http.Error(w, "error creating build", http.StatusInternalServerError)
		return
	}
	cmd.Env = append(cmd.Env, "GOPATH="+gopath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		// We use http status 412 to indicate compile errors.
		w.WriteHeader(412)
		w.Write(out)
		return
	}

	f, err := os.Open(filepath.Join(root, buildID))
	if err != nil {
		log.Println("Error opening executable:", err)
		http.Error(w, "error with build", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := io.Copy(w, f); err != nil {
		log.Println("Error sending executable to client:", err)
	}
}

func (s *Server) buildGOPATH(breq *BuildRequest, root string) error {
	for _, pkg := range breq.Packages {
		if err := os.MkdirAll(filepath.Join(root, "src", pkg.Name), 0755); err != nil {
			return err
		}
		for _, file := range pkg.Files {
			cached := s.Cache.Path(file.Hash)
			dest := filepath.Join(root, "src", pkg.Name, file.Name)
			if err := os.Link(cached, dest); err != nil {
				return err
			}
		}
	}
	return nil
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
