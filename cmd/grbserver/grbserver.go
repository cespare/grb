package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/cespare/grb/internal/grb"
	"github.com/cespare/hutil/apachelog"
)

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

	server, err := grb.NewServer(*dataDir, *goroot)
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
