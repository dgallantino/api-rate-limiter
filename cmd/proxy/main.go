package main

import (
	"flag"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"os"

	"github.com/dgallantino/api-rate-limiter/internal/proxyconfig"
	"github.com/dgallantino/api-rate-limiter/pkg/httplimit"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	path := flag.String("config", "configs/proxy.example.yaml", "path to YAML proxy config")
	flag.Parse()

	cfg, err := proxyconfig.Load(*path)
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	conn, client, err := httplimit.Dial(cfg.CheckAddr)
	if err != nil {
		slog.Error("dial check", "addr", cfg.CheckAddr, "err", err)
		os.Exit(1)
	}
	defer conn.Close()

	h := wrapOrigin(cfg, client)
	slog.Info("proxy listening", "addr", cfg.ListenAddr, "origin", cfg.OriginURL.String(), "check", cfg.CheckAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, h); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}

func wrapOrigin(cfg *proxyconfig.Config, client httplimit.CheckClient) http.Handler {
	rp := httputil.NewSingleHostReverseProxy(cfg.OriginURL)
	return httplimit.Middleware(httplimit.Options{
		Client:  client,
		KeyFunc: cfg.KeyFunc,
		Cost:    cfg.Cost,
		Fail:    cfg.Fail,
		OnDeny: func(key string, remaining, retryAfterMs int64) {
			slog.Info("deny", "key", key, "remaining", remaining, "retry_after_ms", retryAfterMs)
		},
		OnCheckDown: func(key string, err error) {
			slog.Info("check down", "key", key, "err", err)
		},
	})(rp)
}
