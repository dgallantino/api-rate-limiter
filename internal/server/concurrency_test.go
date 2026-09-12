package server

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
)

func TestConcurrencyExactlyMAllowed(t *testing.T) {
	const (
		M    = 10
		N    = 50
		runs = 10
	)
	ctx := context.Background()
	for run := 0; run < runs; run++ {
		env := startTestServer(t, &config.Config{
			Default: config.Policy{Limit: M, Window: time.Minute, Fail: config.FailClosed},
		})
		var allowed atomic.Int64
		var wg sync.WaitGroup
		wg.Add(N)
		for i := 0; i < N; i++ {
			go func() {
				defer wg.Done()
				res, err := env.client.Check(ctx, &checkv1.CheckRequest{Key: "k", Cost: 1})
				if err != nil {
					t.Errorf("check: %v", err)
					return
				}
				if res.Allowed {
					allowed.Add(1)
				}
			}()
		}
		wg.Wait()
		env.stop()
		if got := allowed.Load(); got != M {
			t.Fatalf("run %d: allowed %d, want %d", run, got, M)
		}
	}
}
