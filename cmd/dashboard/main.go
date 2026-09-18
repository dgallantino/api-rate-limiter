package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"

	"github.com/dgallantino/api-rate-limiter/internal/dashconfig"
	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

//go:embed web
var webFS embed.FS

func main() {
	path := flag.String("config", "configs/dashboard.example.yaml", "path to YAML dashboard config")
	flag.Parse()

	cfg, err := dashconfig.Load(*path)
	if err != nil {
		log.Fatal(err)
	}

	conn, err := grpc.NewClient(cfg.CheckAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	pages, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("dashboard listening on %s (check %s)", cfg.ListenAddr, cfg.CheckAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, newMux(checkv1.NewCheckerClient(conn), pages)))
}
