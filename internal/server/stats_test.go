package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	checkv1 "github.com/dgallantino/api-rate-limiter/internal/gen/check/v1"
	"github.com/dgallantino/api-rate-limiter/internal/stats"
)

func TestStatsAllowDenyCounts(t *testing.T) {
	env := startTestServer(t, &config.Config{
		Default: config.Policy{Limit: 2, Window: time.Minute, Fail: config.FailClosed},
	})
	defer env.stop()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		res, err := env.client.Check(ctx, &checkv1.CheckRequest{Key: "k", Cost: 1})
		if err != nil || !res.Allowed {
			t.Fatalf("allow %d: %+v %v", i, res, err)
		}
	}
	res, err := env.client.Check(ctx, &checkv1.CheckRequest{Key: "k", Cost: 1})
	if err != nil || res.Allowed {
		t.Fatalf("deny: %+v %v", res, err)
	}
	snap, err := env.client.Stats(ctx, &checkv1.StatsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Allowed != 2 || snap.Blocked != 1 {
		t.Fatalf("stats: %+v", snap)
	}
	if !snap.RedisUp {
		t.Fatal("redis_up")
	}
	if len(snap.Keys) != 1 || snap.Keys[0].Key != "k" || snap.Keys[0].Limit != 2 || snap.Keys[0].Fail != "closed" {
		t.Fatalf("keys: %+v", snap.Keys)
	}
}

func TestStatsRedisDownLatches(t *testing.T) {
	env := startTestServer(t, &config.Config{
		Default: config.Policy{Limit: 10, Window: time.Minute, Fail: config.FailOpen},
	})
	defer env.stop()
	env.mr.Close()
	ctx := context.Background()
	res, err := env.client.Check(ctx, &checkv1.CheckRequest{Key: "free:demo", Cost: 1})
	if err != nil || !res.Allowed {
		t.Fatalf("fail-open check: %+v %v", res, err)
	}
	snap, err := env.client.Stats(ctx, &checkv1.StatsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if snap.RedisUp {
		t.Fatal("expected redis_up false after store failure")
	}
	if snap.Allowed != 1 || len(snap.Keys) != 1 || snap.Keys[0].Fail != "open" {
		t.Fatalf("snap: %+v", snap)
	}
}

func TestStatsCap50(t *testing.T) {
	env := startTestServer(t, &config.Config{
		Default: config.Policy{Limit: 100, Window: time.Minute, Fail: config.FailClosed},
	})
	defer env.stop()
	ctx := context.Background()
	for i := 0; i < stats.MaxKeys+1; i++ {
		_, err := env.client.Check(ctx, &checkv1.CheckRequest{Key: fmt.Sprintf("k%d", i), Cost: 1})
		if err != nil {
			t.Fatal(err)
		}
	}
	snap, err := env.client.Stats(ctx, &checkv1.StatsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Keys) != stats.MaxKeys {
		t.Fatalf("len=%d", len(snap.Keys))
	}
	for _, k := range snap.Keys {
		if k.Key == "k0" {
			t.Fatal("oldest key should be evicted")
		}
	}
}
