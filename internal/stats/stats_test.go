package stats

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestObserveCountsAndKeys guards that Observe counts allows and blocks,
// records per-key used/remaining/limit/fail most-recent first, and that
// redis_up starts true so the dashboard is not red before any traffic.
func TestObserveCountsAndKeys(t *testing.T) {
	rec := New()
	rec.Observe("free:a", true, 19, 20, "open")
	rec.Observe("pro:b", false, 0, 500, "closed")
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

// TestCap50EvictsOldest guards that the per-key table never exceeds MaxKeys
// and evicts the least recently observed key, so a key-spraying client cannot
// grow Check's memory without bound.
func TestCap50EvictsOldest(t *testing.T) {
	rec := New()
	for i := 0; i < MaxKeys+1; i++ {
		rec.Observe(fmt.Sprintf("k%d", i), true, 1, 10, "closed")
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

// TestRPSLastCompletedBucket guards that RPS reports the last completed
// one-second bucket, not the partial current one, so the dashboard does not
// show a number that ramps up from zero every second.
func TestRPSLastCompletedBucket(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	rec := newRecorder(func() time.Time { return now })
	rec.Observe("k", true, 1, 10, "closed")
	rec.Observe("k", false, 0, 10, "closed")
	if rec.Snapshot().RPS != 0 {
		t.Fatal("current bucket is not yet complete")
	}
	now = now.Add(time.Second)
	if rec.Snapshot().RPS != 2 {
		t.Fatalf("rps=%d want 2", rec.Snapshot().RPS)
	}
}

// TestPrometheusObserveAndRedisGauge guards that Observe feeds the requests
// and blocked counters, that SetRedisUp drives the redis_up gauge, and that
// the registry holds exactly three families. Observe deliberately has no
// effect on the gauge: only the engine breaker reports Redis state.
func TestPrometheusObserveAndRedisGauge(t *testing.T) {
	rec := New()
	reg := prometheus.NewRegistry()
	if err := rec.Register(reg); err != nil {
		t.Fatal(err)
	}
	rec.Observe("a", true, 1, 10, "closed")
	rec.Observe("b", false, 0, 10, "closed")
	rec.Observe("c", true, 0, 10, "open")
	rec.SetRedisUp(false)
	if got := testutil.ToFloat64(rec.reqTotal); got != 3 {
		t.Fatalf("requests_total=%v", got)
	}
	if got := testutil.ToFloat64(rec.blockedTotal); got != 1 {
		t.Fatalf("blocked_total=%v", got)
	}
	if got := testutil.ToFloat64(rec.redisGauge); got != 0 {
		t.Fatalf("redis_up=%v", got)
	}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(mfs) != 3 {
		t.Fatalf("families=%d", len(mfs))
	}
}

func TestOnRedisChangeTransitionsOnly(t *testing.T) {
	rec := New()
	var flips []bool
	rec.SetOnRedisChange(func(up bool) { flips = append(flips, up) })
	rec.SetRedisUp(true)
	rec.SetRedisUp(true)
	rec.SetRedisUp(false)
	rec.SetRedisUp(false)
	rec.SetRedisUp(true)
	if len(flips) != 2 || flips[0] || !flips[1] {
		t.Fatalf("flips=%v", flips)
	}
}
