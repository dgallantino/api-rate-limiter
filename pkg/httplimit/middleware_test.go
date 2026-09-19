package httplimit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stubClient struct {
	check func(context.Context, *checkv1.CheckRequest) (*checkv1.CheckResponse, error)
}

func (s stubClient) Check(ctx context.Context, in *checkv1.CheckRequest, _ ...grpc.CallOption) (*checkv1.CheckResponse, error) {
	return s.check(ctx, in)
}

func hit(t *testing.T, h http.Handler, hdr http.Header) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for k, vs := range hdr {
		req.Header[k] = vs
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func wrap(opts Options, called *bool) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusNoContent)
	})
	return Middleware(opts)(next)
}

func TestAllow(t *testing.T) {
	var gotKey string
	var gotCost int64
	var called bool
	h := wrap(Options{
		Client: stubClient{func(_ context.Context, in *checkv1.CheckRequest) (*checkv1.CheckResponse, error) {
			gotKey, gotCost = in.Key, in.Cost
			return &checkv1.CheckResponse{Allowed: true, Remaining: 7}, nil
		}},
		Cost: 1,
	}, &called)
	rec := hit(t, h, http.Header{"X-Api-Key": []string{"free:demo"}})
	if rec.Code != http.StatusNoContent || !called {
		t.Fatalf("allow: code=%d called=%v", rec.Code, called)
	}
	if rec.Header().Get("X-RateLimit-Remaining") != "7" {
		t.Fatalf("remaining: %q", rec.Header().Get("X-RateLimit-Remaining"))
	}
	if gotKey != "free:demo" || gotCost != 1 {
		t.Fatalf("check req key=%q cost=%d", gotKey, gotCost)
	}
}

func TestDeny(t *testing.T) {
	var called bool
	h := wrap(Options{
		Client: stubClient{func(context.Context, *checkv1.CheckRequest) (*checkv1.CheckResponse, error) {
			return &checkv1.CheckResponse{Allowed: false, Remaining: 0, RetryAfterMs: 1500}, nil
		}},
		Cost: 1,
	}, &called)
	rec := hit(t, h, http.Header{"X-Api-Key": []string{"k"}})
	if rec.Code != http.StatusTooManyRequests || called {
		t.Fatalf("deny: code=%d called=%v", rec.Code, called)
	}
	if rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("retry-after: %q", rec.Header().Get("Retry-After"))
	}
	if rec.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("remaining: %q", rec.Header().Get("X-RateLimit-Remaining"))
	}
	body, _ := io.ReadAll(rec.Body)
	if strings.TrimSpace(string(body)) != denyBody {
		t.Fatalf("body: %s", body)
	}
}

func TestMissingKey(t *testing.T) {
	var called bool
	h := wrap(Options{
		Client: stubClient{func(context.Context, *checkv1.CheckRequest) (*checkv1.CheckResponse, error) {
			t.Fatal("check must not run")
			return nil, nil
		}},
		Cost: 1,
	}, &called)
	rec := hit(t, h, nil)
	if rec.Code != http.StatusBadRequest || called {
		t.Fatalf("missing key: code=%d called=%v", rec.Code, called)
	}
}

func TestFailClosedOnCheckError(t *testing.T) {
	var called bool
	h := wrap(Options{
		Client: stubClient{func(context.Context, *checkv1.CheckRequest) (*checkv1.CheckResponse, error) {
			return nil, errors.New("check down")
		}},
		Cost: 1,
		Fail: FailClosed,
	}, &called)
	rec := hit(t, h, http.Header{"X-Api-Key": []string{"k"}})
	if rec.Code != http.StatusTooManyRequests || called {
		t.Fatalf("fail-closed: code=%d called=%v", rec.Code, called)
	}
	if rec.Header().Get("Retry-After") != "" {
		t.Fatalf("retry-after should be omitted: %q", rec.Header().Get("Retry-After"))
	}
}

func TestFailOpenOnCheckError(t *testing.T) {
	var called bool
	h := wrap(Options{
		Client: stubClient{func(context.Context, *checkv1.CheckRequest) (*checkv1.CheckResponse, error) {
			return nil, status.Error(codes.Unavailable, "check down")
		}},
		Cost: 1,
		Fail: FailOpen,
	}, &called)
	rec := hit(t, h, http.Header{"X-Api-Key": []string{"k"}})
	if rec.Code != http.StatusNoContent || !called {
		t.Fatalf("fail-open: code=%d called=%v", rec.Code, called)
	}
}

func TestInvalidArgument(t *testing.T) {
	var called bool
	h := wrap(Options{
		Client: stubClient{func(context.Context, *checkv1.CheckRequest) (*checkv1.CheckResponse, error) {
			return nil, status.Error(codes.InvalidArgument, "key is required")
		}},
		Cost: 1,
	}, &called)
	rec := hit(t, h, http.Header{"X-Api-Key": []string{"k"}})
	if rec.Code != http.StatusBadRequest || called {
		t.Fatalf("invalid argument: code=%d called=%v", rec.Code, called)
	}
}

func TestParseKeySourceAndIP(t *testing.T) {
	fn, err := ParseKeySource("header:X-API-Key")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "abc")
	if k, err := fn(req); err != nil || k != "abc" {
		t.Fatalf("header: %q %v", k, err)
	}
	fn, err = ParseKeySource("ip")
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	if k, err := fn(req); err != nil || k != "192.0.2.1" {
		t.Fatalf("ip: %q %v", k, err)
	}
	if _, err := ParseKeySource("cookie"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseFail(t *testing.T) {
	if m, err := ParseFail(""); err != nil || m != FailClosed {
		t.Fatalf("default: %v %v", m, err)
	}
	if m, err := ParseFail("open"); err != nil || m != FailOpen {
		t.Fatalf("open: %v %v", m, err)
	}
	if _, err := ParseFail("maybe"); err == nil {
		t.Fatal("expected error")
	}
}

func TestOnDenyCallback(t *testing.T) {
	var gotKey string
	var gotRemaining, gotRetry int64
	var called bool
	h := wrap(Options{
		Client: stubClient{func(context.Context, *checkv1.CheckRequest) (*checkv1.CheckResponse, error) {
			return &checkv1.CheckResponse{Allowed: false, Remaining: 0, RetryAfterMs: 1500}, nil
		}},
		Cost: 1,
		OnDeny: func(key string, remaining, retryAfterMs int64) {
			gotKey, gotRemaining, gotRetry = key, remaining, retryAfterMs
		},
	}, &called)
	rec := hit(t, h, http.Header{"X-Api-Key": []string{"free:demo"}})
	if rec.Code != http.StatusTooManyRequests || called {
		t.Fatalf("deny: code=%d called=%v", rec.Code, called)
	}
	if gotKey != "free:demo" || gotRemaining != 0 || gotRetry != 1500 {
		t.Fatalf("onDeny key=%q remaining=%d retry=%d", gotKey, gotRemaining, gotRetry)
	}
}

func TestOnCheckDownCallback(t *testing.T) {
	var gotKey string
	var gotErr error
	var called bool
	h := wrap(Options{
		Client: stubClient{func(context.Context, *checkv1.CheckRequest) (*checkv1.CheckResponse, error) {
			return nil, status.Error(codes.Unavailable, "check down")
		}},
		Cost: 1,
		Fail: FailOpen,
		OnCheckDown: func(key string, err error) {
			gotKey, gotErr = key, err
		},
	}, &called)
	rec := hit(t, h, http.Header{"X-Api-Key": []string{"k"}})
	if rec.Code != http.StatusNoContent || !called {
		t.Fatalf("fail-open: code=%d called=%v", rec.Code, called)
	}
	if gotKey != "k" || gotErr == nil {
		t.Fatalf("onCheckDown key=%q err=%v", gotKey, gotErr)
	}
}

func TestCostFuncPeek(t *testing.T) {
	var got int64 = -1
	var called bool
	h := wrap(Options{
		Client: stubClient{func(_ context.Context, in *checkv1.CheckRequest) (*checkv1.CheckResponse, error) {
			got = in.Cost
			return &checkv1.CheckResponse{Allowed: true, Remaining: 3}, nil
		}},
		Cost:     1,
		CostFunc: func(*http.Request) int64 { return 0 },
	}, &called)
	rec := hit(t, h, http.Header{"X-Api-Key": []string{"k"}})
	if rec.Code != http.StatusNoContent || !called || got != 0 {
		t.Fatalf("peek: code=%d called=%v cost=%d", rec.Code, called, got)
	}
}
