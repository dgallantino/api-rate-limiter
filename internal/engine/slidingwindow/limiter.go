package slidingwindow

import (
	"context"
	"embed"
	"fmt"
	"sync"
	"time"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/dgallantino/api-rate-limiter/internal/engine"
	"github.com/redis/go-redis/v9"
)

// shedAfter is how long one Check may sit inside Redis before later Checks
// stop waiting and latch Redis down. Healthy evals are far under this.
const shedAfter = 50 * time.Millisecond

//go:embed script.lua
var luaFS embed.FS

var script = redis.NewScript(mustReadLua())

type Limiter struct {
	rdb      redis.Cmdable
	now      func() time.Time
	redisUp  func() bool
	markDown func()

	mu       sync.Mutex
	inflight int
	oldest   time.Time
}

func New(rdb redis.Cmdable) *Limiter {
	return &Limiter{rdb: rdb, now: time.Now}
}

// WithBreaker skips Redis while up is false. One Check already inside Redis
// for shedAfter makes later Checks fail immediately and calls markDown.
// A successful Check does not call markDown; the PING loop raises the latch.
func (l *Limiter) WithBreaker(up func() bool, markDown func()) *Limiter {
	l.redisUp = up
	l.markDown = markDown
	return l
}

var _ engine.Checker = (*Limiter)(nil)

func (l *Limiter) Check(ctx context.Context, key string, cost int64, policy config.Policy) (engine.Result, error) {
	if l.latchedDown() {
		return storeDown(policy), nil
	}
	now := l.now()
	if l.redisUp != nil {
		if !l.acquire(now) {
			l.trip()
			return storeDown(policy), nil
		}
		defer l.release()
	}
	res, err := l.eval(ctx, key, cost, policy, now)
	if err != nil {
		l.trip()
		return storeDown(policy), nil
	}
	return res, nil
}

func (l *Limiter) latchedDown() bool {
	return l.redisUp != nil && !l.redisUp()
}

func (l *Limiter) acquire(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inflight > 0 && now.Sub(l.oldest) >= shedAfter {
		return false
	}
	if l.inflight == 0 {
		l.oldest = now
	}
	l.inflight++
	return true
}

func (l *Limiter) release() {
	l.mu.Lock()
	l.inflight--
	l.mu.Unlock()
}

func (l *Limiter) trip() {
	if l.markDown != nil {
		l.markDown()
	}
}

func storeDown(policy config.Policy) engine.Result {
	if policy.Fail == config.FailOpen {
		return engine.Result{Allowed: true, StoreFailed: true}
	}
	return engine.Result{StoreFailed: true}
}

func (l *Limiter) eval(ctx context.Context, key string, cost int64, policy config.Policy, now time.Time) (engine.Result, error) {
	raw, err := script.Run(ctx, l.rdb, []string{"rl:" + key},
		now.UnixMilli(), policy.Window.Milliseconds(), policy.Limit, cost,
	).Slice()
	if err != nil {
		return engine.Result{}, err
	}
	if len(raw) != 3 {
		return engine.Result{}, fmt.Errorf("slidingwindow: unexpected lua result %#v", raw)
	}
	allowed, err := toInt(raw[0])
	if err != nil {
		return engine.Result{}, err
	}
	remaining, err := toInt(raw[1])
	if err != nil {
		return engine.Result{}, err
	}
	retry, err := toInt(raw[2])
	if err != nil {
		return engine.Result{}, err
	}
	return engine.Result{Allowed: allowed == 1, Remaining: remaining, RetryAfterMs: retry}, nil
}

func toInt(v any) (int64, error) {
	switch n := v.(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case float64:
		return int64(n), nil
	default:
		return 0, fmt.Errorf("slidingwindow: not an int: %T", v)
	}
}

func mustReadLua() string {
	b, err := luaFS.ReadFile("script.lua")
	if err != nil {
		panic(err)
	}
	return string(b)
}
