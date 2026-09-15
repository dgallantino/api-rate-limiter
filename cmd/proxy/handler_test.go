package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"github.com/dgallantino/api-rate-limiter/internal/proxyconfig"
	"github.com/dgallantino/api-rate-limiter/pkg/httplimit"
	"google.golang.org/grpc"
)

type stubClient struct {
	allowed bool
}

func (s stubClient) Check(context.Context, *checkv1.CheckRequest, ...grpc.CallOption) (*checkv1.CheckResponse, error) {
	return &checkv1.CheckResponse{Allowed: s.allowed, Remaining: 0, RetryAfterMs: 1000}, nil
}

func TestWrapOriginDenyDoesNotHitOrigin(t *testing.T) {
	var originHits int
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		originHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(origin.Close)
	u, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &proxyconfig.Config{
		OriginURL: u,
		KeyFunc:   httplimit.KeyFromHeader("X-API-Key"),
		Cost:      1,
		Fail:      httplimit.FailClosed,
	}

	h := wrapOrigin(cfg, stubClient{allowed: false})
	req := httptest.NewRequest(http.MethodGet, "/work", nil)
	req.Header.Set("X-API-Key", "free:demo")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests || originHits != 0 {
		t.Fatalf("deny: code=%d originHits=%d", rec.Code, originHits)
	}

	h = wrapOrigin(cfg, stubClient{allowed: true})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || originHits != 1 || string(body) != "ok" {
		t.Fatalf("allow: code=%d hits=%d body=%s", rec.Code, originHits, body)
	}
}
