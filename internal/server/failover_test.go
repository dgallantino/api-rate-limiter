package server

import (
	"context"
	"testing"
	"time"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
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
