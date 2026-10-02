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

// Breaker gates the Limiter's Redis calls. Real Checks open it (store error,
// or one Check inside Redis for shedAfter) and close it (the single half-open
// probe succeeds). Watch only moves open to half-open; a PING never closes or
// opens it.
type Breaker struct {
	onChange func(up bool)

	mu       sync.Mutex
	state    breakerState
	gen      uint64
	inflight int
	oldest   time.Time
}

// ticket ties a Check's result to the breaker generation it entered under.
// Results from an older generation are ignored.
type ticket struct {
	gen uint64
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
		if b.inflight == 0 {
			b.inflight = 1
			b.oldest = now
			return ticket{gen: b.gen}, true
		}
		if now.Sub(b.oldest) >= shedAfter {
			b.setLocked(open)
		}
		return ticket{}, false
	}
	if b.inflight > 0 && now.Sub(b.oldest) >= shedAfter {
		b.setLocked(open)
		b.notifyLocked(false)
		return ticket{}, false
	}
	if b.inflight == 0 {
		b.oldest = now
	}
	b.inflight++
	return ticket{gen: b.gen}, true
}

func (b *Breaker) exit(t ticket, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t.gen != b.gen {
		return
	}
	b.inflight--
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

func (b *Breaker) setLocked(s breakerState) {
	b.state = s
	b.gen++
	b.inflight = 0
}

func (b *Breaker) notifyLocked(up bool) {
	if b.onChange != nil {
		b.onChange(up)
	}
}
