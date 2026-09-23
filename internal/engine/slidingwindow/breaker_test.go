package slidingwindow

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/redis/go-redis/v9"
)

func TestLatchedDownSkipsRedis(t *testing.T) {
	l, mr := setup(t)
	var up atomic.Bool
	l.WithBreaker(up.Load, func() { up.Store(false) })

	r, err := l.Check(context.Background(), "k", 1, pol(2, time.Minute, config.FailOpen))
	if err != nil || !r.Allowed || r.Remaining != 0 || r.RetryAfterMs != 0 || !r.StoreFailed {
		t.Fatalf("open: %+v %v", r, err)
	}
	r, err = l.Check(context.Background(), "k", 1, pol(2, time.Minute, config.FailClosed))
	if err != nil || r.Allowed || r.Remaining != 0 || r.RetryAfterMs != 0 || !r.StoreFailed {
		t.Fatalf("closed: %+v %v", r, err)
	}
	if mr.Exists("rl:k") {
		t.Fatal("latched down must not call redis")
	}
	if up.Load() {
		t.Fatal("a skipped check must not raise redis")
	}
}

func TestStoreErrorTripsBreaker(t *testing.T) {
	l, mr := setup(t)
	var up atomic.Bool
	up.Store(true)
	l.WithBreaker(up.Load, func() { up.Store(false) })
	mr.Close()

	r, err := l.Check(context.Background(), "k", 1, pol(2, time.Minute, config.FailClosed))
	if err != nil || r.Allowed || !r.StoreFailed {
		t.Fatalf("trip: %+v %v", r, err)
	}
	if up.Load() {
		t.Fatal("store error must latch redis down")
	}
	if mr.Exists("rl:k") {
		t.Fatal("failed check must not leave a key")
	}

	r, err = l.Check(context.Background(), "k", 1, pol(2, time.Minute, config.FailOpen))
	if err != nil || !r.Allowed || !r.StoreFailed {
		t.Fatalf("skip: %+v %v", r, err)
	}
	if up.Load() {
		t.Fatal("later check must not raise redis")
	}
}

func TestHangShedsLaterChecks(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	accepted := make(chan struct{}, 1)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			select {
			case accepted <- struct{}{}:
			default:
			}
		}
	}()
	rdb := redis.NewClient(&redis.Options{
		Addr:                  ln.Addr().String(),
		DialTimeout:           100 * time.Millisecond,
		ReadTimeout:           5 * time.Second,
		WriteTimeout:          100 * time.Millisecond,
		PoolTimeout:           100 * time.Millisecond,
		MaxRetries:            -1,
		DialerRetries:         1,
		ContextTimeoutEnabled: true,
	})
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
		_ = rdb.Close()
	})

	var up atomic.Bool
	up.Store(true)
	l := New(rdb).WithBreaker(up.Load, func() { up.Store(false) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = l.Check(ctx, "slow", 1, pol(2, time.Minute, config.FailClosed))
	}()
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("expected redis dial")
	}
	time.Sleep(shedAfter + 20*time.Millisecond)

	start := time.Now()
	r, err := l.Check(context.Background(), "later", 1, pol(2, time.Minute, config.FailClosed))
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("shed took %s", elapsed)
	}
	if err != nil || r.Allowed || !r.StoreFailed {
		t.Fatalf("shed: %+v %v", r, err)
	}
	if up.Load() {
		t.Fatal("shed must latch redis down")
	}
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	n := len(conns)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("dials=%d, latched checks must not dial", n)
	}

	cancel()
	ln.Close()
	mu.Lock()
	for _, c := range conns {
		_ = c.Close()
	}
	mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("hung check did not return")
	}
}
