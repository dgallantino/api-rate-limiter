package slidingwindow

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/dgallantino/api-rate-limiter/internal/engine"
	"github.com/redis/go-redis/v9"
)

//go:embed script.lua
var luaFS embed.FS

var script = redis.NewScript(mustReadLua())

type Limiter struct {
	rdb redis.Cmdable
	now func() time.Time
}

func New(rdb redis.Cmdable) *Limiter {
	return &Limiter{rdb: rdb, now: time.Now}
}

var _ engine.Checker = (*Limiter)(nil)

func (l *Limiter) Check(ctx context.Context, key string, cost int64, policy config.Policy) (engine.Result, error) {
	now := l.now()
	res, err := l.eval(ctx, key, cost, policy, now)
	if err == nil {
		return res, nil
	}
	if policy.Fail == config.FailOpen {
		return engine.Result{Allowed: true}, nil
	}
	return engine.Result{}, nil
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
