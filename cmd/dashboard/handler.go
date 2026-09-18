package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"

	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"google.golang.org/grpc"
)

type statsClient interface {
	Stats(ctx context.Context, in *checkv1.StatsRequest, opts ...grpc.CallOption) (*checkv1.StatsSnapshot, error)
}

type keyJSON struct {
	Key       string `json:"key"`
	Used      int64  `json:"used"`
	Remaining int64  `json:"remaining"`
	Limit     int64  `json:"limit"`
	Fail      string `json:"fail"`
}

type statsJSON struct {
	Allowed int64     `json:"allowed"`
	Blocked int64     `json:"blocked"`
	RPS     int64     `json:"rps"`
	RedisUp bool      `json:"redis_up"`
	Keys    []keyJSON `json:"keys"`
	Error   string    `json:"error,omitempty"`
}

func newMux(client statsClient, pages fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		serveStats(w, r, client)
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(pages, "index.html")
		if err != nil {
			http.Error(w, "index not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
	})
	return mux
}

func serveStats(w http.ResponseWriter, r *http.Request, client statsClient) {
	w.Header().Set("Content-Type", "application/json")
	snap, err := client.Stats(r.Context(), &checkv1.StatsRequest{})
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(statsJSON{Error: "check unreachable"})
		return
	}
	keys := make([]keyJSON, 0, len(snap.GetKeys()))
	for _, k := range snap.GetKeys() {
		keys = append(keys, keyJSON{
			Key:       k.GetKey(),
			Used:      k.GetUsed(),
			Remaining: k.GetRemaining(),
			Limit:     k.GetLimit(),
			Fail:      k.GetFail(),
		})
	}
	_ = json.NewEncoder(w).Encode(statsJSON{
		Allowed: snap.GetAllowed(),
		Blocked: snap.GetBlocked(),
		RPS:     snap.GetRps(),
		RedisUp: snap.GetRedisUp(),
		Keys:    keys,
	})
}
