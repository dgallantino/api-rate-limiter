package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"google.golang.org/grpc"
)

type stubClient struct {
	calls   int
	allowed bool
}

func (s *stubClient) Check(context.Context, *checkv1.CheckRequest, ...grpc.CallOption) (*checkv1.CheckResponse, error) {
	s.calls++
	return &checkv1.CheckResponse{Allowed: s.allowed, Remaining: 0, RetryAfterMs: 1000}, nil
}

func TestHealthNeverCallsCheck(t *testing.T) {
	stub := &stubClient{allowed: false}
	h := newHandler(true, stub)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || stub.calls != 0 || strings.TrimSpace(string(body)) != `{"status":"ok"}` {
		t.Fatalf("health: code=%d calls=%d body=%s", rec.Code, stub.calls, body)
	}
}

func TestLimitedDenyDoesNotRunWork(t *testing.T) {
	stub := &stubClient{allowed: false}
	h := newHandler(true, stub)
	req := httptest.NewRequest(http.MethodGet, "/work", nil)
	req.Header.Set("X-API-Key", "free:demo")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusTooManyRequests || stub.calls != 1 {
		t.Fatalf("deny: code=%d calls=%d", rec.Code, stub.calls)
	}
	if strings.Contains(string(body), `"ok"`) {
		t.Fatalf("deny ran work: body=%s", body)
	}
}

func TestLimitedAllowRunsWork(t *testing.T) {
	stub := &stubClient{allowed: true}
	h := newHandler(true, stub)
	req := httptest.NewRequest(http.MethodGet, "/work", nil)
	req.Header.Set("X-API-Key", "free:demo")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || stub.calls != 1 || strings.TrimSpace(string(body)) != `{"ok":true}` {
		t.Fatalf("allow: code=%d calls=%d body=%s", rec.Code, stub.calls, body)
	}
}

func TestUnlimitedWorkDoesNotCallCheck(t *testing.T) {
	stub := &stubClient{allowed: false}
	h := newHandler(false, stub)
	req := httptest.NewRequest(http.MethodGet, "/work", nil)
	req.Header.Set("X-API-Key", "free:demo")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || stub.calls != 0 || strings.TrimSpace(string(body)) != `{"ok":true}` {
		t.Fatalf("unlimited: code=%d calls=%d body=%s", rec.Code, stub.calls, body)
	}
}
