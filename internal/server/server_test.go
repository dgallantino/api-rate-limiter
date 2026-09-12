package server

import (
	"context"
	"testing"
	"time"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/dgallantino/api-rate-limiter/internal/engine"
	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stub struct{ res engine.Result }

func (s stub) Check(context.Context, string, int64, config.Policy) (engine.Result, error) {
	return s.res, nil
}

func TestCheckInvalidArgument(t *testing.T) {
	s := New(&config.Config{Default: config.Policy{Limit: 10, Window: time.Minute}}, stub{})
	ctx := context.Background()
	_, err := s.Check(ctx, &checkv1.CheckRequest{Cost: 1})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty key: %v", err)
	}
	_, err = s.Check(ctx, &checkv1.CheckRequest{Key: "k", Cost: -1})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("neg cost: %v", err)
	}
}
