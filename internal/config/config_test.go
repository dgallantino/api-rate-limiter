package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleYAML = `
listen_addr: ":50051"
redis:
  addr: "127.0.0.1:6379"
default:
  limit: 100
  window: 1m
  fail: closed
keys:
  vip-client:
    limit: 1000
    window: 1m
    fail: closed
prefixes:
  "pro:":
    limit: 500
    window: 1m
    fail: closed
  "pro:eu:":
    limit: 250
    window: 30s
    fail: open
`

const sampleJSON = `{
  "listen_addr": ":50051",
  "redis": {"addr": "127.0.0.1:6379"},
  "default": {"limit": 100, "window": "1m", "fail": "closed"},
  "keys": {
    "vip-client": {"limit": 1000, "window": "1m", "fail": "closed"}
  },
  "prefixes": {
    "pro:": {"limit": 500, "window": "1m", "fail": "closed"},
    "pro:eu:": {"limit": 250, "window": "30s", "fail": "open"}
  }
}`

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadYAMLAndLookup(t *testing.T) {
	cfg, err := Load(writeTemp(t, "check.yaml", sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":50051" || cfg.Redis.Addr != "127.0.0.1:6379" || cfg.MetricsAddr != "" {
		t.Fatalf("listen/redis/metrics: %+v", cfg)
	}
	cases := []struct {
		key   string
		limit int64
		win   time.Duration
		fail  FailMode
	}{
		{"vip-client", 1000, time.Minute, FailClosed},
		{"pro:eu:user", 250, 30 * time.Second, FailOpen},
		{"pro:us:user", 500, time.Minute, FailClosed},
		{"other", 100, time.Minute, FailClosed},
	}
	for _, tc := range cases {
		p := cfg.Lookup(tc.key)
		if p.Limit != tc.limit || p.Window != tc.win || p.Fail != tc.fail {
			t.Fatalf("%s: got %+v", tc.key, p)
		}
	}
}

func TestLoadMetricsAddr(t *testing.T) {
	body := sampleYAML + "metrics_addr: \":2112\"\n"
	cfg, err := Load(writeTemp(t, "check.yaml", body))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MetricsAddr != ":2112" {
		t.Fatalf("metrics_addr=%q", cfg.MetricsAddr)
	}
}

func TestLoadJSON(t *testing.T) {
	cfg, err := Load(writeTemp(t, "check.json", sampleJSON))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Lookup("vip-client").Limit != 1000 {
		t.Fatalf("json keys: %+v", cfg.Lookup("vip-client"))
	}
	if cfg.Lookup("pro:eu:x").Limit != 250 {
		t.Fatalf("json prefix: %+v", cfg.Lookup("pro:eu:x"))
	}
}

func TestLoadValidation(t *testing.T) {
	valid := strings.TrimSpace(sampleYAML)
	tests := []struct {
		name string
		body string
		want string
	}{
		{"missing default", "listen_addr: \":1\"\nredis:\n  addr: a\n", "default"},
		{"empty prefix", valid + "\n  \"\":\n    limit: 1\n    window: 1s\n    fail: closed\n", "empty prefix"},
		{"bad window", strings.Replace(valid, "window: 1m", "window: nope", 1), "window"},
		{"bad ext", "listen_addr: x", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name := "c.yaml"
			if tc.name == "bad ext" {
				name = "c.toml"
			}
			_, err := Load(writeTemp(t, name, tc.body))
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestFailModeString(t *testing.T) {
	if FailOpen.String() != "open" || FailClosed.String() != "closed" {
		t.Fatalf("open=%q closed=%q", FailOpen.String(), FailClosed.String())
	}
}
