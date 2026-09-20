package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/dgallantino/api-rate-limiter/pkg/httplimit"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	addr := flag.String("addr", ":8000", "listen address")
	limited := flag.Bool("limited", false, "wrap non-health routes with httplimit middleware")
	checkAddr := flag.String("check", envOr("CHECK_ADDR", "127.0.0.1:50051"), "Check gRPC address")
	flag.Parse()

	var client httplimit.CheckClient
	if *limited {
		conn, c, err := httplimit.Dial(*checkAddr)
		if err != nil {
			slog.Error("dial check", "addr", *checkAddr, "err", err)
			os.Exit(1)
		}
		defer conn.Close()
		client = c
	}

	h := newHandler(*limited, client)
	if *limited {
		slog.Info("origin listening", "addr", *addr, "limited", true, "check", *checkAddr)
	} else {
		slog.Info("origin listening", "addr", *addr, "limited", false)
	}
	if err := http.ListenAndServe(*addr, h); err != nil {
		slog.Error("serve", "err", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func newHandler(limited bool, client httplimit.CheckClient) http.Handler {
	app := http.NewServeMux()
	app.HandleFunc("GET /health", health)
	app.HandleFunc("GET /work", work)
	if !limited {
		return app
	}
	if client == nil {
		panic("origin: limited handler requires Check client")
	}
	wrapped := httplimit.Middleware(httplimit.Options{
		Client:  client,
		KeyFunc: httplimit.KeyFromHeader("X-API-Key"),
		Cost:    1,
		Fail:    httplimit.FailClosed,
		OnDeny: func(key string, remaining, retryAfterMs int64) {
			slog.Info("deny", "key", key, "remaining", remaining, "retry_after_ms", retryAfterMs)
		},
		OnCheckDown: func(key string, err error) {
			slog.Info("check down", "key", key, "err", err)
		},
	})(app)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/health" {
			app.ServeHTTP(w, r)
			return
		}
		wrapped.ServeHTTP(w, r)
	})
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func work(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}
