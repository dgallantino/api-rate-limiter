package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseKeys(t *testing.T) {
	got := parseKeys(" free:demo, pro:demo , ")
	if len(got) != 2 || got[0] != "free:demo" || got[1] != "pro:demo" {
		t.Fatalf("%q", got)
	}
}

func TestHitCounts(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if r.Header.Get("X-API-Key") != "free:demo" {
			t.Errorf("header=%q", r.Header.Get("X-API-Key"))
		}
		if n == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "ok")
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	totals := &counters{}
	per := &counters{}
	hit(t.Context(), srv.Client(), srv.URL, "X-API-Key", "free:demo", totals, per)
	hit(t.Context(), srv.Client(), srv.URL, "X-API-Key", "free:demo", totals, per)
	if totals.allowed.Load() != 1 || totals.denied.Load() != 1 || per.allowed.Load() != 1 {
		t.Fatalf("totals allow=%d deny=%d", totals.allowed.Load(), totals.denied.Load())
	}

	var b strings.Builder
	printReport(&b, []string{"free:demo"}, totals, []*counters{per})
	out := b.String()
	if !strings.Contains(out, "allowed: 1") || !strings.Contains(out, "denied:  1") {
		t.Fatalf("%s", out)
	}
}
