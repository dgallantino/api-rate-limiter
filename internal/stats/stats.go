package stats

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

const MaxKeys = 50

type KeyStat struct {
	Key       string
	Used      int64
	Remaining int64
	Limit     int64
	Fail      string
}

type Snapshot struct {
	Allowed int64
	Blocked int64
	RPS     int64
	RedisUp bool
	Keys    []KeyStat
}

type Recorder struct {
	allowed atomic.Int64
	blocked atomic.Int64
	redisUp atomic.Bool
	now     func() time.Time

	mu          sync.Mutex
	bucketStart time.Time
	bucketCount int64
	lastRPS     int64
	lru         *list.List
	byKey       map[string]*list.Element
}

type keyEntry struct {
	stat KeyStat
}

func New() *Recorder {
	return newRecorder(time.Now)
}

func newRecorder(now func() time.Time) *Recorder {
	r := &Recorder{
		now:   now,
		lru:   list.New(),
		byKey: make(map[string]*list.Element),
	}
	r.redisUp.Store(true)
	r.bucketStart = now().Truncate(time.Second)
	return r
}

func (r *Recorder) Observe(key string, allowed bool, remaining, limit int64, fail string, storeFailed bool) {
	if storeFailed {
		r.SetRedisUp(false)
	}
	if allowed {
		r.allowed.Add(1)
	} else {
		r.blocked.Add(1)
	}
	used := limit - remaining
	if used < 0 {
		used = 0
	}
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rotateLocked(now)
	r.bucketCount++
	r.touchLocked(key, used, remaining, limit, fail)
}

func (r *Recorder) SetRedisUp(up bool) {
	r.redisUp.Store(up)
}

func (r *Recorder) Snapshot() Snapshot {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rotateLocked(now)
	keys := make([]KeyStat, 0, r.lru.Len())
	for e := r.lru.Front(); e != nil; e = e.Next() {
		keys = append(keys, e.Value.(*keyEntry).stat)
	}
	return Snapshot{
		Allowed: r.allowed.Load(),
		Blocked: r.blocked.Load(),
		RPS:     r.lastRPS,
		RedisUp: r.redisUp.Load(),
		Keys:    keys,
	}
}

func (r *Recorder) rotateLocked(now time.Time) {
	sec := now.Truncate(time.Second)
	if !sec.After(r.bucketStart) {
		return
	}
	if sec.Sub(r.bucketStart) == time.Second {
		r.lastRPS = r.bucketCount
	} else {
		r.lastRPS = 0
	}
	r.bucketCount = 0
	r.bucketStart = sec
}

func (r *Recorder) touchLocked(key string, used, remaining, limit int64, fail string) {
	if e, ok := r.byKey[key]; ok {
		e.Value.(*keyEntry).stat = KeyStat{
			Key:       key,
			Used:      used,
			Remaining: remaining,
			Limit:     limit,
			Fail:      fail,
		}
		r.lru.MoveToFront(e)
		return
	}
	e := r.lru.PushFront(&keyEntry{stat: KeyStat{
		Key:       key,
		Used:      used,
		Remaining: remaining,
		Limit:     limit,
		Fail:      fail,
	}})
	r.byKey[key] = e
	for r.lru.Len() > MaxKeys {
		back := r.lru.Back()
		r.lru.Remove(back)
		delete(r.byKey, back.Value.(*keyEntry).stat.Key)
	}
}
