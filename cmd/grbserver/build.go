package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/cespare/grb/internal/grb"
)

func (s *Server) Build(w http.ResponseWriter, buildID string, breq *grb.BuildRequest) {
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

func (s *Server) buildGOPATH(breq *grb.BuildRequest, root string) error {
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
