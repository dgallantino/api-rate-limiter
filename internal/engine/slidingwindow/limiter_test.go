package slidingwindow

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/redis/go-redis/v9"
)

func setup(t *testing.T) (*Limiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	l := New(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	return l, mr
}

func pol(limit int64, window time.Duration, fail config.FailMode) config.Policy {
	return config.Policy{Limit: limit, Window: window, Fail: fail}
}

func TestAllowDenyPeek(t *testing.T) {
	l, mr := setup(t)
	ctx := context.Background()
	p := pol(2, time.Minute, config.FailClosed)

	r, err := l.Check(ctx, "k", 1, p)
	if err != nil || !r.Allowed || r.Remaining != 1 {
		t.Fatalf("first: %+v %v", r, err)
	}
	r, err = l.Check(ctx, "k", 0, p)
	if err != nil || !r.Allowed || r.Remaining != 1 {
		t.Fatalf("peek: %+v %v", r, err)
	}
	if mr.Exists("rl:k") == false {
		t.Fatal("expected key after consume")
	}
	r, err = l.Check(ctx, "k", 1, p)
	if err != nil || !r.Allowed || r.Remaining != 0 {
		t.Fatalf("second: %+v %v", r, err)
	}
	r, err = l.Check(ctx, "k", 1, p)
	if err != nil || r.Allowed || r.Remaining != 0 || r.RetryAfterMs <= 0 {
		t.Fatalf("deny: %+v %v", r, err)
	}
	r, err = l.Check(ctx, "k", 0, p)
	if err != nil || r.Allowed || r.Remaining != 0 {
		t.Fatalf("peek full: %+v %v", r, err)
	}
}

func TestPeekDoesNotCreate(t *testing.T) {
	l, mr := setup(t)
	r, err := l.Check(context.Background(), "missing", 0, pol(5, time.Minute, config.FailClosed))
	if err != nil || !r.Allowed || r.Remaining != 5 {
		t.Fatalf("peek empty: %+v %v", r, err)
	}
	if mr.Exists("rl:missing") {
		t.Fatal("peek must not create a key")
	}
}

func TestCostExceedsLimit(t *testing.T) {
	l, _ := setup(t)
	r, err := l.Check(context.Background(), "k", 10, pol(3, time.Minute, config.FailClosed))
	if err != nil || r.Allowed || r.RetryAfterMs != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestWindowRoll(t *testing.T) {
	l, _ := setup(t)
	now := time.Unix(0, 0).UTC()
	l.now = func() time.Time { return now }
	ctx := context.Background()
	p := pol(2, 10*time.Second, config.FailClosed)
	if r, _ := l.Check(ctx, "k", 2, p); !r.Allowed {
		t.Fatal(r)
	}
	if r, _ := l.Check(ctx, "k", 1, p); r.Allowed {
		t.Fatal("expected deny in first window")
	}
	now = now.Add(15 * time.Second)
	r, err := l.Check(ctx, "k", 1, p)
	if err != nil || !r.Allowed {
		t.Fatalf("halfway next window: %+v %v", r, err)
	}
}

func TestFailModes(t *testing.T) {
	l, mr := setup(t)
	ctx := context.Background()
	mr.Close()
	r, err := l.Check(ctx, "k", 1, pol(2, time.Minute, config.FailOpen))
	if err != nil || !r.Allowed || r.Remaining != 0 || r.RetryAfterMs != 0 {
		t.Fatalf("open: %+v %v", r, err)
	}
	r, err = l.Check(ctx, "k", 1, pol(2, time.Minute, config.FailClosed))
	if err != nil || r.Allowed || r.Remaining != 0 || r.RetryAfterMs != 0 {
		t.Fatalf("closed: %+v %v", r, err)
	}
}
