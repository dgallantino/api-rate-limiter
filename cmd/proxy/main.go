package main

import (
	"flag"
	"log"
	"net/http"
	"net/http/httputil"

	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"github.com/dgallantino/api-rate-limiter/internal/proxyconfig"
	"github.com/dgallantino/api-rate-limiter/pkg/httplimit"
)

func main() {
	path := flag.String("config", "configs/proxy.example.yaml", "path to YAML proxy config")
	flag.Parse()

	cfg, err := proxyconfig.Load(*path)
	if err != nil {
		log.Fatal(err)
	}

	conn, client, err := httplimit.Dial(cfg.CheckAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	h := wrapOrigin(cfg, client)
	log.Printf("proxy listening on %s -> %s (check %s)", cfg.ListenAddr, cfg.OriginURL, cfg.CheckAddr)
	log.Fatal(http.ListenAndServe(cfg.ListenAddr, h))
}

func wrapOrigin(cfg *proxyconfig.Config, client checkv1.CheckerClient) http.Handler {
	rp := httputil.NewSingleHostReverseProxy(cfg.OriginURL)
	return httplimit.Middleware(httplimit.Options{
		Client:  client,
		KeyFunc: cfg.KeyFunc,
		Cost:    cfg.Cost,
		Fail:    cfg.Fail,
	})(rp)
}
