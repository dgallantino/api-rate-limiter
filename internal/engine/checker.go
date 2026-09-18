package engine

import (
	"context"

	"github.com/dgallantino/api-rate-limiter/internal/config"
)

type Result struct {
	Allowed      bool
	Remaining    int64
	RetryAfterMs int64
	StoreFailed  bool
}

type Checker interface {
	Check(ctx context.Context, key string, cost int64, policy config.Policy) (Result, error)
}
