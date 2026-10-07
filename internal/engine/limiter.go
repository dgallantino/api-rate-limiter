package engine

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/redis/go-redis/v9"
)

// errBadResult is a script reply this process cannot use. Redis answered, so
// it is not a store failure and must not open the breaker.
var errBadResult = errors.New("engine: bad script result")

// shedAfter is how long one Check may sit inside Redis before later Checks
// stop waiting and open the breaker. Healthy evals are far under this.
const shedAfter = 50 * time.Millisecond

//go:embed script.lua
var luaFS embed.FS

var script = redis.NewScript(mustReadLua())

type Limiter struct {
	rdb redis.Cmdable
	now func() time.Time
	br  *Breaker
}

func New(rdb redis.Cmdable) *Limiter {
	return &Limiter{rdb: rdb, now: time.Now}
}

// WithBreaker skips Redis while b is open or half-open with its probe in flight.
func (l *Limiter) WithBreaker(b *Breaker) *Limiter {
	l.br = b
	return l
}

var _ Checker = (*Limiter)(nil)

func (l *Limiter) Check(ctx context.Context, key string, cost int64, policy config.Policy) (Result, error) {
	now := l.now()
	var t ticket
	if l.br != nil {
		var ok bool
		if t, ok = l.br.enter(now); !ok {
			return storeDown(policy), nil
		}
	}
	res, err := l.eval(ctx, key, cost, policy, now)
	if errors.Is(err, errBadResult) {
		if l.br != nil {
			l.br.release(t)
		}
		return Result{}, err
	}
	if l.br != nil {
		l.br.exit(t, err == nil)
	}
	if err != nil {
		return storeDown(policy), nil
	}
	return res, nil
}

func storeDown(policy config.Policy) Result {
	if policy.Fail == config.FailOpen {
		return Result{Allowed: true, StoreFailed: true}
	}
	return Result{StoreFailed: true}
}

func (l *Limiter) eval(ctx context.Context, key string, cost int64, policy config.Policy, now time.Time) (Result, error) {
	raw, err := script.Run(ctx, l.rdb, []string{"rl:" + key},
		now.UnixMilli(), policy.Window.Milliseconds(), policy.Limit, cost,
	).Slice()
	if err != nil {
		return Result{}, err
	}
	if len(raw) != 3 {
		return Result{}, fmt.Errorf("%w: %#v", errBadResult, raw)
	}
	allowed, err := toInt(raw[0])
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", errBadResult, err)
	}
	remaining, err := toInt(raw[1])
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", errBadResult, err)
	}
	retry, err := toInt(raw[2])
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", errBadResult, err)
	}
	return Result{Allowed: allowed == 1, Remaining: remaining, RetryAfterMs: retry}, nil
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
		return 0, fmt.Errorf("engine: not an int: %T", v)
	}
}

func mustReadLua() string {
	b, err := luaFS.ReadFile("script.lua")
	if err != nil {
		panic(err)
	}
	return string(b)
}
