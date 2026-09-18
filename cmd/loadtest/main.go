package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type counters struct {
	allowed atomic.Int64
	denied  atomic.Int64
	errors  atomic.Int64
}

func main() {
	url := flag.String("url", "http://127.0.0.1:8080/work", "HTTP URL to hit")
	rate := flag.Float64("rate", 20, "total requests per second")
	duration := flag.Duration("duration", 30*time.Second, "how long to run")
	header := flag.String("header", "X-API-Key", "header name for the API key")
	keysFlag := flag.String("keys", "free:demo,pro:demo", "comma-separated keys to rotate")
	flag.Parse()

	keys := parseKeys(*keysFlag)
	if len(keys) == 0 {
		log.Fatal("loadtest: at least one key is required")
	}
	if *rate <= 0 {
		log.Fatal("loadtest: rate must be > 0")
	}
	if *duration <= 0 {
		log.Fatal("loadtest: duration must be > 0")
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        128,
			MaxIdleConnsPerHost: 128,
		},
	}

	totals := &counters{}
	perKey := make([]*counters, len(keys))
	for i := range perKey {
		perKey[i] = &counters{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	interval := time.Duration(float64(time.Second) / *rate)
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var wg sync.WaitGroup
	var i int
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			printReport(os.Stdout, keys, totals, perKey)
			return
		case <-ticker.C:
			idx := i % len(keys)
			i++
			key := keys[idx]
			c := perKey[idx]
			wg.Add(1)
			go func() {
				defer wg.Done()
				hit(ctx, client, *url, *header, key, totals, c)
			}()
		}
	}
}

func parseKeys(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func hit(ctx context.Context, client *http.Client, url, header, key string, totals, per *counters) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		totals.errors.Add(1)
		per.errors.Add(1)
		return
	}
	req.Header.Set(header, key)
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		totals.errors.Add(1)
		per.errors.Add(1)
		return
	}
	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()
	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		totals.denied.Add(1)
		per.denied.Add(1)
	case res.StatusCode >= 200 && res.StatusCode < 300:
		totals.allowed.Add(1)
		per.allowed.Add(1)
	default:
		totals.errors.Add(1)
		per.errors.Add(1)
	}
}

func printReport(w io.Writer, keys []string, totals *counters, perKey []*counters) {
	fmt.Fprintf(w, "allowed: %d\n", totals.allowed.Load())
	fmt.Fprintf(w, "denied:  %d\n", totals.denied.Load())
	fmt.Fprintf(w, "errors:  %d\n", totals.errors.Load())
	for i, key := range keys {
		fmt.Fprintf(w, "  %s allow=%d deny=%d err=%d\n",
			key, perKey[i].allowed.Load(), perKey[i].denied.Load(), perKey[i].errors.Load())
	}
}
