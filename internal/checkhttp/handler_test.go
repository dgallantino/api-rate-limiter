package checkhttp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dgallantino/api-rate-limiter/internal/stats"
	"github.com/prometheus/client_golang/prometheus"
)

func TestMetricsAndHealthz(t *testing.T) {
	rec := stats.New()
	reg := prometheus.NewRegistry()
	if err := rec.Register(reg); err != nil {
		t.Fatal(err)
	}
	h := Handler(reg)

	rec.Observe("free:a", true, 19, 20, "open", false)
	rec.Observe("pro:b", false, 0, 500, "closed", false)
	rec.SetRedisUp(false)

	health := httptest.NewRecorder()
	h.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("healthz code=%d", health.Code)
	}
	body, _ := io.ReadAll(health.Body)
	if strings.TrimSpace(string(body)) != "ok" {
		t.Fatalf("healthz body=%q", body)
	}

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(mfs) != 3 {
		t.Fatalf("metric families=%d want 3", len(mfs))
	}
	got := map[string]float64{}
	for _, mf := range mfs {
		if len(mf.Metric) == 0 {
			continue
		}
		m := mf.Metric[0]
		if m.Counter != nil {
			got[mf.GetName()] = m.Counter.GetValue()
		}
		if m.Gauge != nil {
			got[mf.GetName()] = m.Gauge.GetValue()
		}
	}
	if got[stats.MetricRequestsTotal] != 2 || got[stats.MetricBlockedTotal] != 1 || got[stats.MetricRedisUp] != 0 {
		t.Fatalf("series=%v", got)
	}

	metrics := httptest.NewRecorder()
	h.ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metrics.Code != http.StatusOK {
		t.Fatalf("metrics code=%d", metrics.Code)
	}
	text := metrics.Body.String()
	for _, name := range []string{stats.MetricRequestsTotal, stats.MetricBlockedTotal, stats.MetricRedisUp} {
		if !strings.Contains(text, name) {
			t.Fatalf("missing %s in %s", name, text)
		}
	}
	if strings.Contains(text, "go_goroutines") || strings.Contains(text, "promhttp_metric_handler_errors_total") {
		t.Fatalf("unexpected collectors in %s", text)
	}
}
