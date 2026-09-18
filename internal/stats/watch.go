package stats

import (
	"context"
	"time"
)

func WatchRedis(ctx context.Context, ping func(context.Context) error, rec *Recorder, every time.Duration) {
	if every <= 0 {
		every = time.Second
	}
	do := func() {
		c, cancel := context.WithTimeout(ctx, every)
		defer cancel()
		if err := ping(c); err != nil {
			rec.SetRedisUp(false)
			return
		}
		rec.SetRedisUp(true)
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
