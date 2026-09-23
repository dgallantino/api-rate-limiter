package server

import (
	"context"
	"os"
	"path/filepath"
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

type capture struct {
	policy config.Policy
	res    engine.Result
}

func (c *capture) Check(_ context.Context, _ string, _ int64, policy config.Policy) (engine.Result, error) {
	c.policy = policy
	return c.res, nil
}

func TestCheckInvalidArgument(t *testing.T) {
	s := New(config.NewStore(&config.Config{Default: config.Policy{Limit: 10, Window: time.Minute}}), stub{}, nil)
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

func TestSetLimitThenCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "check.yaml")
	body := []byte("listen_addr: \":1\"\nredis:\n  addr: localhost:6379\ndefault:\n  limit: 10\n  window: 1m\n  fail: closed\nprefixes:\n  \"free:\":\n    limit: 20\n    window: 1m\n    fail: open\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := &capture{res: engine.Result{Allowed: true, Remaining: 2}}
	s := New(config.NewStore(cfg), got, nil)
	ctx := context.Background()
	res, err := s.SetLimit(ctx, &checkv1.SetLimitRequest{
		Target: checkv1.PolicyTarget_POLICY_TARGET_PREFIX,
		Name:   "free:",
		Limit:  4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.GetRule().GetLimit() != 4 || res.GetRule().GetWindow() != "1m" || res.GetRule().GetFail() != "open" {
		t.Fatalf("rule: %+v", res.GetRule())
	}
	if _, err := s.Check(ctx, &checkv1.CheckRequest{Key: "free:demo", Cost: 1}); err != nil {
		t.Fatal(err)
	}
	if got.policy.Limit != 4 || got.policy.Fail != config.FailOpen {
		t.Fatalf("next check policy: %+v", got.policy)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Lookup("free:demo").Limit != 4 || reloaded.Lookup("other").Limit != 10 {
		t.Fatalf("reloaded free=%d other=%d", reloaded.Lookup("free:demo").Limit, reloaded.Lookup("other").Limit)
	}
	_, err = s.SetLimit(ctx, &checkv1.SetLimitRequest{
		Target: checkv1.PolicyTarget_POLICY_TARGET_KEY,
		Name:   "missing",
		Limit:  5,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing key: %v", err)
	}
	if s.store.Lookup("free:demo").Limit != 4 {
		t.Fatalf("lookup changed: %d", s.store.Lookup("free:demo").Limit)
	}
	_, err = s.SetLimit(ctx, &checkv1.SetLimitRequest{Limit: 0})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("bad target: %v", err)
	}
}
