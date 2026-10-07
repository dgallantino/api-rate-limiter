package server

import (
	"context"
	"testing"
	"time"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/dgallantino/api-rate-limiter/internal/engine"
	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"github.com/dgallantino/api-rate-limiter/internal/stats"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestFailClosedWhenRedisDown(t *testing.T) {
	env := startTestServer(t, &config.Config{
		Default: config.Policy{Limit: 10, Window: time.Minute, Fail: config.FailClosed},
	})
	defer env.stop()
	env.mr.Close()

	res, err := env.client.Check(context.Background(), &checkv1.CheckRequest{Key: "k", Cost: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Allowed || res.Remaining != 0 || res.RetryAfterMs != 0 {
		t.Fatalf("fail-closed: %+v", res)
	}
}

func TestFailOpenWhenRedisDown(t *testing.T) {
	env := startTestServer(t, &config.Config{
		Default: config.Policy{Limit: 10, Window: time.Minute, Fail: config.FailOpen},
	})
	defer env.stop()
	env.mr.Close()

	res, err := env.client.Check(context.Background(), &checkv1.CheckRequest{Key: "k", Cost: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Allowed || res.Remaining != 0 || res.RetryAfterMs != 0 {
		t.Fatalf("fail-open: %+v", res)
	}
}

type replyHook struct {
	val any
}

func (replyHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h replyHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		switch cmd.Name() {
		case "eval", "evalsha":
			c, ok := cmd.(*redis.Cmd)
			if !ok {
				return next(ctx, cmd)
			}
			c.SetVal(h.val)
			return nil
		default:
			return next(ctx, cmd)
		}
	}
}

func (replyHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func TestBadScriptResultIsInternal(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0"})
	rdb.AddHook(replyHook{val: []any{int64(1)}})
	t.Cleanup(func() { _ = rdb.Close() })
	rec := stats.New()
	lim := engine.New(rdb).WithBreaker(engine.NewBreaker(rec.SetRedisUp))
	s := New(config.NewStore(&config.Config{
		Default: config.Policy{Limit: 10, Window: time.Minute, Fail: config.FailOpen},
	}), lim, rec)

	res, err := s.Check(context.Background(), &checkv1.CheckRequest{Key: "k", Cost: 1})
	if status.Code(err) != codes.Internal || res != nil {
		t.Fatalf("res=%v err=%v", res, err)
	}
	snap := rec.Snapshot()
	if !snap.RedisUp || snap.Allowed != 0 || snap.Blocked != 0 {
		t.Fatalf("snap: %+v", snap)
	}
}
