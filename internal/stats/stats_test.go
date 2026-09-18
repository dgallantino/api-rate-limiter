package stats

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestObserveCountsAndKeys(t *testing.T) {
	rec := New()
	rec.Observe("free:a", true, 19, 20, "open", false)
	rec.Observe("pro:b", false, 0, 500, "closed", false)
	snap := rec.Snapshot()
	if snap.Allowed != 1 || snap.Blocked != 1 {
		t.Fatalf("totals: %+v", snap)
	}
	if !snap.RedisUp {
		t.Fatal("redis_up should start true")
	}
	if len(snap.Keys) != 2 {
		t.Fatalf("keys: %+v", snap.Keys)
	}
	if snap.Keys[0].Key != "pro:b" || snap.Keys[0].Used != 500 || snap.Keys[0].Fail != "closed" {
		t.Fatalf("most recent: %+v", snap.Keys[0])
	}
	if snap.Keys[1].Key != "free:a" || snap.Keys[1].Used != 1 || snap.Keys[1].Remaining != 19 {
		t.Fatalf("older: %+v", snap.Keys[1])
	}
}

func TestStoreFailedLatchesRedisDown(t *testing.T) {
	rec := New()
	rec.Observe("k", true, 0, 20, "open", true)
	if rec.Snapshot().RedisUp {
		t.Fatal("expected redis_up false")
	}
	rec.Observe("k", true, 19, 20, "open", false)
	if rec.Snapshot().RedisUp {
		t.Fatal("successful check must not raise redis_up")
	}
}

func TestCap50EvictsOldest(t *testing.T) {
	rec := New()
	for i := 0; i < MaxKeys+1; i++ {
		rec.Observe(fmt.Sprintf("k%d", i), true, 1, 10, "closed", false)
	}
	snap := rec.Snapshot()
	if len(snap.Keys) != MaxKeys {
		t.Fatalf("len=%d", len(snap.Keys))
	}
	for _, k := range snap.Keys {
		if k.Key == "k0" {
			t.Fatal("oldest key should be evicted")
		}
	}
}

func TestRPSLastCompletedBucket(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	rec := newRecorder(func() time.Time { return now })
	rec.Observe("k", true, 1, 10, "closed", false)
	rec.Observe("k", false, 0, 10, "closed", false)
	if rec.Snapshot().RPS != 0 {
		t.Fatal("current bucket is not yet complete")
	}
	now = now.Add(time.Second)
	if rec.Snapshot().RPS != 2 {
		t.Fatalf("rps=%d want 2", rec.Snapshot().RPS)
	}
}

func TestWatchRedisPing(t *testing.T) {
	rec := New()
	rec.SetRedisUp(false)
	ctx, cancel := context.WithCancel(context.Background())
	pings := make(chan struct{}, 4)
	go WatchRedis(ctx, func(context.Context) error {
		select {
		case pings <- struct{}{}:
		default:
		}
		return nil
	}, rec, 20*time.Millisecond)
	select {
	case <-pings:
	case <-time.After(time.Second):
		t.Fatal("expected ping")
	}
	waitRedis(t, rec, true)
	cancel()

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go WatchRedis(ctx2, func(context.Context) error {
		return errors.New("down")
	}, rec, 20*time.Millisecond)
	waitRedis(t, rec, false)
}

func waitRedis(t *testing.T, rec *Recorder, up bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if rec.Snapshot().RedisUp == up {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("redis_up=%v want %v", rec.Snapshot().RedisUp, up)
}
