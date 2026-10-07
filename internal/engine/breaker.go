package engine

import (
	"context"
	"sync"
	"time"
)

type breakerState int

const (
	closed breakerState = iota
	open
	halfOpen
)

// Breaker gates the Limiter's Redis calls. A store error opens it. One Check
// inside Redis for shedAfter sheds later Checks and does not open it. The
// single half-open probe closes it on success. Watch only moves open to
// half-open; a PING never closes or opens it.
type Breaker struct {
	onChange func(up bool)

	mu    sync.Mutex
	state breakerState
	gen   uint64
	live  []time.Time
}

// ticket ties a Check's result to the generation and start time it entered
// under. Results from an older generation are ignored.
type ticket struct {
	gen   uint64
	start time.Time
}

// NewBreaker starts closed. onChange fires on up/down flips only and runs
// under the breaker lock, so it must not call back into the Breaker.
func NewBreaker(onChange func(up bool)) *Breaker {
	return &Breaker{onChange: onChange}
}

func (b *Breaker) enter(now time.Time) (ticket, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case open:
		return ticket{}, false
	case halfOpen:
		if len(b.live) == 0 {
			return b.admitLocked(now), true
		}
		if oldest, ok := b.oldestLive(); ok && now.Sub(oldest) >= shedAfter {
			b.setLocked(open)
		}
		return ticket{}, false
	}
	if oldest, ok := b.oldestLive(); ok && now.Sub(oldest) >= shedAfter {
		return ticket{}, false
	}
	return b.admitLocked(now), true
}

func (b *Breaker) admitLocked(now time.Time) ticket {
	b.live = append(b.live, now)
	return ticket{gen: b.gen, start: now}
}

// release drops the in-flight Check for this generation and leaves the state
// alone. A bad script result is not a store failure and not a successful eval.
func (b *Breaker) release(t ticket) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t.gen != b.gen {
		return
	}
	b.dropLive(t.start)
}

func (b *Breaker) exit(t ticket, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t.gen != b.gen {
		return
	}
	b.dropLive(t.start)
	switch b.state {
	case closed:
		if !ok {
			b.setLocked(open)
			b.notifyLocked(false)
		}
	case halfOpen:
		if ok {
			b.setLocked(closed)
			b.notifyLocked(true)
			return
		}
		b.setLocked(open)
	}
}

// Watch pings every interval while the breaker is open and moves it to
// half-open on the first success. It blocks until ctx is done.
func (b *Breaker) Watch(ctx context.Context, ping func(context.Context) error, every time.Duration) {
	if every <= 0 {
		every = time.Second
	}
	do := func() {
		if !b.isOpen() {
			return
		}
		c, cancel := context.WithTimeout(ctx, every)
		defer cancel()
		if ping(c) == nil {
			b.nudge()
		}
	}
	do()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			do()
		}
	}
}

func (b *Breaker) isOpen() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state == open
}

func (b *Breaker) nudge() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == open {
		b.setLocked(halfOpen)
	}
}

func (b *Breaker) oldestLive() (time.Time, bool) {
	if len(b.live) == 0 {
		return time.Time{}, false
	}
	oldest := b.live[0]
	for _, start := range b.live[1:] {
		if start.Before(oldest) {
			oldest = start
		}
	}
	return oldest, true
}

func (b *Breaker) dropLive(start time.Time) {
	for i, s := range b.live {
		if s.Equal(start) {
			b.live = append(b.live[:i], b.live[i+1:]...)
			return
		}
	}
}

func (b *Breaker) setLocked(s breakerState) {
	b.state = s
	b.gen++
	b.live = nil
}

func (b *Breaker) notifyLocked(up bool) {
	if b.onChange != nil {
		b.onChange(up)
	}
}
