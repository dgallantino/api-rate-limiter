package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stubStats struct {
	snap *checkv1.StatsSnapshot
	err  error
}

func (s stubStats) Stats(context.Context, *checkv1.StatsRequest, ...grpc.CallOption) (*checkv1.StatsSnapshot, error) {
	return s.snap, s.err
}

func TestStatsJSON(t *testing.T) {
	pages := fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}
	h := newMux(stubStats{snap: &checkv1.StatsSnapshot{
		Allowed: 3,
		Blocked: 1,
		Rps:     4,
		RedisUp: true,
		Keys: []*checkv1.KeyStat{{
			Key:       "free:demo",
			Used:      3,
			Remaining: 17,
			Limit:     20,
			Fail:      "open",
		}},
	}}, pages)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var got statsJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Allowed != 3 || got.Blocked != 1 || got.RPS != 4 || !got.RedisUp || len(got.Keys) != 1 {
		t.Fatalf("%+v", got)
	}
	if got.Keys[0].Key != "free:demo" || got.Keys[0].Fail != "open" || got.Keys[0].Used != 3 {
		t.Fatalf("key: %+v", got.Keys[0])
	}
}

func TestStatsUnreachable(t *testing.T) {
	h := newMux(stubStats{err: status.Error(codes.Unavailable, "down")}, fstest.MapFS{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stats", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code=%d", rec.Code)
	}
	var got statsJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error != "check unreachable" {
		t.Fatalf("%+v", got)
	}
}

func TestIndex(t *testing.T) {
	pages := fstest.MapFS{"index.html": {Data: []byte("<html>dash</html>")}}
	h := newMux(stubStats{}, pages)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "<html>dash</html>" {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("ctype=%s", rec.Header().Get("Content-Type"))
	}
}
