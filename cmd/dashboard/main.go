package main

import (
	"embed"
	"flag"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/dgallantino/api-rate-limiter/internal/dashconfig"
	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

//go:embed web
var webFS embed.FS

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	path := flag.String("config", "configs/dashboard.example.yaml", "path to YAML dashboard config")
	flag.Parse()

	cfg, err := dashconfig.Load(*path)
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	conn, err := grpc.NewClient(cfg.CheckAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("dial check", "addr", cfg.CheckAddr, "err", err)
		os.Exit(1)
	}
	defer conn.Close()

	pages, err := fs.Sub(webFS, "web")
	if err != nil {
		slog.Error("web", "err", err)
		os.Exit(1)
	}

	slog.Info("dashboard listening", "addr", cfg.ListenAddr, "check", cfg.CheckAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, newMux(checkv1.NewCheckerClient(conn), pages)); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}
