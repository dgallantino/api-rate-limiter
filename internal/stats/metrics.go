package stats

import "github.com/prometheus/client_golang/prometheus"

const (
	MetricRequestsTotal = "api_rate_limiter_requests_total"
	MetricBlockedTotal  = "api_rate_limiter_blocked_total"
	MetricRedisUp       = "api_rate_limiter_redis_up"
)

func (r *Recorder) initMetrics() {
	r.reqTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: MetricRequestsTotal,
		Help: "Total Check observations (allowed and blocked).",
	})
	r.blockedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: MetricBlockedTotal,
		Help: "Total Check observations that were not allowed.",
	})
	r.redisGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: MetricRedisUp,
		Help: "1 if Redis is reachable, 0 otherwise.",
	})
	r.redisGauge.Set(1)
}

func (r *Recorder) Register(reg prometheus.Registerer) error {
	if err := reg.Register(r.reqTotal); err != nil {
		return err
	}
	if err := reg.Register(r.blockedTotal); err != nil {
		return err
	}
	return reg.Register(r.redisGauge)
}
