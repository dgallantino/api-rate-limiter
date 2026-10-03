package engine

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgallantino/api-rate-limiter/internal/config"
	"github.com/redis/go-redis/v9"
)

type flips struct {
	mu sync.Mutex
	v  []bool
}

func (f *flips) add(up bool) {
	f.mu.Lock()
	f.v = append(f.v, up)
	f.mu.Unlock()
}

func (f *flips) want(t *testing.T, want ...bool) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Equal(f.v, want) {
		t.Fatalf("flips=%v want %v", f.v, want)
	}
}

func stateOf(b *Breaker) breakerState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func trip(t *testing.T, b *Breaker) {
	t.Helper()
	tk, ok := b.enter(time.Now())
	if !ok {
		t.Fatal("trip: enter refused")
	}
	b.exit(tk, false)
	if stateOf(b) != open {
		t.Fatalf("trip: state=%v", stateOf(b))
	}
}

// TestOpenSkipsRedis guards that an open breaker answers every Check from the
// policy fail mode (fail-open allows, fail-closed denies, StoreFailed set)
// without touching Redis, and that skipped Checks never report Redis up.
// Skipping is the point of the breaker: Checks must not queue behind a dead store.
func TestOpenSkipsRedis(t *testing.T) {
	l, mr := setup(t)
	var f flips
	b := NewBreaker(f.add)
	l.WithBreaker(b)
	trip(t, b)

	r, err := l.Check(context.Background(), "k", 1, pol(2, time.Minute, config.FailOpen))
	if err != nil || !r.Allowed || r.Remaining != 0 || r.RetryAfterMs != 0 || !r.StoreFailed {
		t.Fatalf("open: %+v %v", r, err)
	}
	r, err = l.Check(context.Background(), "k", 1, pol(2, time.Minute, config.FailClosed))
	if err != nil || r.Allowed || r.Remaining != 0 || r.RetryAfterMs != 0 || !r.StoreFailed {
		t.Fatalf("closed: %+v %v", r, err)
	}
	if mr.Exists("rl:k") {
		t.Fatal("open breaker must not call redis")
	}
	f.want(t, false)
}

// TestStoreErrorOpensUntilNudged guards that a real Check hitting a store
// error opens the breaker and reports down once, and that the breaker stays
// open after Redis is back until something nudges it to half-open.
// There is no cooldown timer, so no real request is spent on a store that
// has not answered a PING yet.
func TestStoreErrorOpensUntilNudged(t *testing.T) {
	l, mr := setup(t)
	var f flips
	b := NewBreaker(f.add)
	l.WithBreaker(b)
	mr.Close()

	r, err := l.Check(context.Background(), "k", 1, pol(2, time.Minute, config.FailClosed))
	if err != nil || r.Allowed || !r.StoreFailed {
		t.Fatalf("trip: %+v %v", r, err)
	}
	f.want(t, false)
	if err := mr.Restart(); err != nil {
		t.Fatal(err)
	}

	r, err = l.Check(context.Background(), "k", 1, pol(2, time.Minute, config.FailOpen))
	if err != nil || !r.Allowed || !r.StoreFailed {
		t.Fatalf("skip: %+v %v", r, err)
	}
	if mr.Exists("rl:k") {
		t.Fatal("open breaker must not call redis before a nudge")
	}
	f.want(t, false)
}

// TestProbeSuccessCloses guards that in half-open the next Check really runs
// against Redis (its result and key are real, not a fail-mode answer), that
// its success closes the breaker and reports up, and that later Checks keep
// counting normally. Only a real EVAL, not a PING, may declare Redis healthy.
func TestProbeSuccessCloses(t *testing.T) {
	l, mr := setup(t)
	var f flips
	b := NewBreaker(f.add)
	l.WithBreaker(b)
	trip(t, b)
	b.nudge()
	if stateOf(b) != halfOpen {
		t.Fatalf("state=%v want halfOpen", stateOf(b))
	}

	r, err := l.Check(context.Background(), "k", 1, pol(2, time.Minute, config.FailClosed))
	if err != nil || !r.Allowed || r.StoreFailed || r.Remaining != 1 {
		t.Fatalf("probe: %+v %v", r, err)
	}
	if !mr.Exists("rl:k") {
		t.Fatal("probe must reach redis")
	}
	if stateOf(b) != closed {
		t.Fatalf("state=%v want closed", stateOf(b))
	}
	f.want(t, false, true)

	r, err = l.Check(context.Background(), "k", 1, pol(2, time.Minute, config.FailClosed))
	if err != nil || !r.Allowed || r.StoreFailed || r.Remaining != 0 {
		t.Fatalf("after close: %+v %v", r, err)
	}
}

// TestProbeFailureReopens guards that a failed half-open probe returns the
// fail-mode answer and puts the breaker back to open without another down
// flip. PING can succeed while EVAL still fails, so a nudge alone must not
// let traffic back in.
func TestProbeFailureReopens(t *testing.T) {
	l, mr := setup(t)
	var f flips
	b := NewBreaker(f.add)
	l.WithBreaker(b)
	trip(t, b)
	mr.Close()
	b.nudge()

	r, err := l.Check(context.Background(), "k", 1, pol(2, time.Minute, config.FailClosed))
	if err != nil || r.Allowed || !r.StoreFailed {
		t.Fatalf("probe: %+v %v", r, err)
	}
	if stateOf(b) != open {
		t.Fatalf("state=%v want open", stateOf(b))
	}
	f.want(t, false)
}

// TestSingleProbeInFlight guards that half-open admits exactly one Check and
// sheds the rest until that probe resolves. Letting a burst through would
// pile load onto a store that may still be recovering, and with fail-closed
// policies every failed probe is a denied request.
func TestSingleProbeInFlight(t *testing.T) {
	var f flips
	b := NewBreaker(f.add)
	trip(t, b)
	b.nudge()
	now := time.Now()

	probe, ok := b.enter(now)
	if !ok {
		t.Fatal("first half-open check must probe")
	}
	if _, ok := b.enter(now); ok {
		t.Fatal("second half-open check must be shed")
	}
	b.exit(probe, true)
	if stateOf(b) != closed {
		t.Fatalf("state=%v want closed", stateOf(b))
	}
	f.want(t, false, true)
}

// TestHungProbeReopens guards that a probe stuck inside Redis for shedAfter
// reopens the breaker on the next Check, and that the hung probe's eventual
// success is ignored. Without this, one hung probe would hold the breaker in
// half-open, shedding everything, until the client read timeout.
func TestHungProbeReopens(t *testing.T) {
	var f flips
	b := NewBreaker(f.add)
	trip(t, b)
	b.nudge()
	now := time.Now()

	probe, ok := b.enter(now)
	if !ok {
		t.Fatal("probe refused")
	}
	if _, ok := b.enter(now.Add(shedAfter)); ok {
		t.Fatal("check behind hung probe must be shed")
	}
	if stateOf(b) != open {
		t.Fatalf("state=%v want open", stateOf(b))
	}
	b.exit(probe, true)
	if stateOf(b) != open {
		t.Fatal("late probe success must not close the breaker")
	}
	f.want(t, false)
}

// TestStaleResultIgnored guards the generation tickets: a Check that entered
// before a state change cannot close the breaker, take the half-open probe
// slot, or resolve the current probe when it finally returns. Slow EVALs that
// started while Redis was healthy would otherwise undo a trip.
func TestStaleResultIgnored(t *testing.T) {
	var f flips
	b := NewBreaker(f.add)
	now := time.Now()

	slow, ok := b.enter(now)
	if !ok {
		t.Fatal("slow refused")
	}
	fast, ok := b.enter(now)
	if !ok {
		t.Fatal("fast refused")
	}
	b.exit(fast, false)
	b.exit(slow, true)
	if stateOf(b) != open {
		t.Fatal("success from before the trip must not close the breaker")
	}

	b.nudge()
	probe, ok := b.enter(now)
	if !ok {
		t.Fatal("stale exit must not consume the probe slot")
	}
	b.exit(slow, true)
	if stateOf(b) != halfOpen {
		t.Fatal("stale exit must not resolve the probe")
	}
	b.exit(probe, true)
	if stateOf(b) != closed {
		t.Fatalf("state=%v want closed", stateOf(b))
	}
	f.want(t, false, true)
}

// TestOnChangeTransitionsOnly guards that onChange sees only up/down flips:
// closed to open reports down, half-open to closed reports up, while open to
// half-open, a failed probe, repeated nudges, and successes while closed
// report nothing. The hook drives the redis_up gauge and the up/down logs,
// so internal state moves must not produce noise there.
func TestOnChangeTransitionsOnly(t *testing.T) {
	var f flips
	b := NewBreaker(f.add)
	trip(t, b)
	b.nudge()
	probe, _ := b.enter(time.Now())
	b.exit(probe, false)
	b.nudge()
	b.nudge()
	probe, _ = b.enter(time.Now())
	b.exit(probe, true)
	ok, _ := b.enter(time.Now())
	b.exit(ok, true)
	f.want(t, false, true)
}

// TestWatchNudgesOnlyWhenOpen guards the PING loop: it does not ping while
// closed, a failed PING leaves the breaker open, and a successful PING only
// moves it to half-open, never straight to closed, and never reports up.
// Real Checks own the up/down decision; PING only says "worth trying again".
func TestWatchNudgesOnlyWhenOpen(t *testing.T) {
	var f flips
	b := NewBreaker(f.add)
	var pings atomic.Int64
	var reachable atomic.Bool
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Watch(ctx, func(context.Context) error {
		pings.Add(1)
		if !reachable.Load() {
			return errors.New("down")
		}
		return nil
	}, 5*time.Millisecond)

	time.Sleep(30 * time.Millisecond)
	if pings.Load() != 0 {
		t.Fatalf("pings=%d, closed breaker must not ping", pings.Load())
	}

	trip(t, b)
	waitPings(t, &pings, 2)
	if stateOf(b) != open {
		t.Fatalf("ping failure must leave breaker open, state=%v", stateOf(b))
	}

	reachable.Store(true)
	deadline := time.Now().Add(time.Second)
	for stateOf(b) != halfOpen {
		if time.Now().After(deadline) {
			t.Fatalf("state=%v want halfOpen", stateOf(b))
		}
		time.Sleep(time.Millisecond)
	}
	f.want(t, false)
}

func waitPings(t *testing.T, pings *atomic.Int64, n int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for pings.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("pings=%d want >= %d", pings.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestYoungerInFlightDoesNotShed guards that shed age is the oldest Check that
// has not returned. A fast Check that already returned must not make a younger
// in-flight Check look shedAfter old, and refusing a Check must not mark Redis
// down. Otherwise a healthy overlap past 50ms latches the breaker and fail-open
// stops enforcing the quota.
func TestYoungerInFlightDoesNotShed(t *testing.T) {
	var f flips
	b := NewBreaker(f.add)
	t0 := time.Now()

	a, ok := b.enter(t0)
	if !ok {
		t.Fatal("A refused")
	}
	if _, ok := b.enter(t0.Add(10 * time.Millisecond)); !ok {
		t.Fatal("B refused")
	}
	b.exit(a, true)

	if _, ok := b.enter(t0.Add(50 * time.Millisecond)); !ok {
		t.Fatal("C refused while the oldest live Check is 40ms old")
	}
	if stateOf(b) != closed {
		t.Fatalf("state=%v want closed", stateOf(b))
	}
	f.want(t)

	if _, ok := b.enter(t0.Add(10*time.Millisecond + shedAfter)); ok {
		t.Fatal("D admitted while the oldest live Check is shedAfter old")
	}
	if stateOf(b) != closed {
		t.Fatalf("shed state=%v want closed", stateOf(b))
	}
	f.want(t)
}

// TestHangShedsLaterChecks guards hang detection against a TCP server that
// accepts and never replies: once one Check has sat in Redis for shedAfter,
// the next Check returns the fail-mode answer immediately and does not dial.
// The shed itself leaves the breaker closed. When the hung Check's context is
// cancelled, that store error opens the breaker and reports down once.
// A hang is not a store error until the call returns.
func TestHangShedsLaterChecks(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	accepted := make(chan struct{}, 1)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			select {
			case accepted <- struct{}{}:
			default:
			}
		}
	}()
	rdb := redis.NewClient(&redis.Options{
		Addr:                  ln.Addr().String(),
		DialTimeout:           100 * time.Millisecond,
		ReadTimeout:           5 * time.Second,
		WriteTimeout:          100 * time.Millisecond,
		PoolTimeout:           100 * time.Millisecond,
		MaxRetries:            -1,
		DialerRetries:         1,
		ContextTimeoutEnabled: true,
	})
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
		_ = rdb.Close()
	})

	var f flips
	b := NewBreaker(f.add)
	l := New(rdb).WithBreaker(b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = l.Check(ctx, "slow", 1, pol(2, time.Minute, config.FailClosed))
	}()
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("expected redis dial")
	}
	time.Sleep(shedAfter + 20*time.Millisecond)

	start := time.Now()
	r, err := l.Check(context.Background(), "later", 1, pol(2, time.Minute, config.FailClosed))
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("shed took %s", elapsed)
	}
	if err != nil || r.Allowed || !r.StoreFailed {
		t.Fatalf("shed: %+v %v", r, err)
	}
	if stateOf(b) != closed {
		t.Fatalf("shed must leave the breaker closed, state=%v", stateOf(b))
	}
	f.want(t)
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	n := len(conns)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("dials=%d, shed checks must not dial", n)
	}

	cancel()
	ln.Close()
	mu.Lock()
	for _, c := range conns {
		_ = c.Close()
	}
	mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("hung check did not return")
	}
	if stateOf(b) != open {
		t.Fatal("hung check store error must open the breaker")
	}
	f.want(t, false)
}
