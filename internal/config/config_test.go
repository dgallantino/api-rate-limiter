package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestSetLimitPersistsYAML(t *testing.T) {
	path := writeTemp(t, "check.yaml", sampleYAML)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	st := NewStore(cfg)
	rule, err := st.SetLimit(TargetPrefix, "pro:", 42)
	if err != nil {
		t.Fatal(err)
	}
	if rule.Policy.Limit != 42 || rule.Policy.Window != time.Minute || rule.Policy.Fail != FailClosed {
		t.Fatalf("rule: %+v", rule)
	}
	if st.Lookup("pro:us").Limit != 42 || st.Lookup("pro:eu:x").Limit != 250 || st.Lookup("vip-client").Limit != 1000 || st.Lookup("other").Limit != 100 {
		t.Fatalf("lookup after set: pro=%d eu=%d vip=%d other=%d", st.Lookup("pro:us").Limit, st.Lookup("pro:eu:x").Limit, st.Lookup("vip-client").Limit, st.Lookup("other").Limit)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Lookup("pro:us").Limit != 42 || loaded.Lookup("pro:eu:x").Window != 30*time.Second || loaded.Lookup("pro:eu:x").Fail != FailOpen {
		t.Fatalf("reloaded pro=%+v eu=%+v", loaded.Lookup("pro:us"), loaded.Lookup("pro:eu:x"))
	}
	var proWindow string
	for _, r := range loaded.list() {
		if r.Target == TargetPrefix && r.Name == "pro:" {
			proWindow = r.Policy.WindowString()
		}
	}
	if proWindow != "1m" {
		t.Fatalf("window string %q", proWindow)
	}

	if _, err := st.SetLimit(TargetKey, "nope", 5); !errors.Is(err, ErrUnknownPolicy) {
		t.Fatalf("unknown key: %v", err)
	}
	if _, err := st.SetLimit(TargetDefault, "", 0); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("bad limit: %v", err)
	}
	if _, err := st.SetLimit(TargetDefault, "x", 9); !errors.Is(err, ErrUnknownPolicy) {
		t.Fatalf("named default: %v", err)
	}
	afterFail, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := st.SetLimit(TargetPrefix, "pro:", 42)
	if err != nil {
		t.Fatal(err)
	}
	if again.Policy.Limit != 42 {
		t.Fatalf("repeat: %+v", again)
	}
	stable, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterFail) != string(stable) {
		t.Fatalf("file churned\n%s\n%s", afterFail, stable)
	}
	if string(before) == string(stable) {
		t.Fatal("file was not rewritten")
	}
	if st.Lookup("other").Limit != 100 {
		t.Fatalf("default changed: %d", st.Lookup("other").Limit)
	}
}

func TestSetLimitPersistsJSON(t *testing.T) {
	path := writeTemp(t, "check.json", sampleJSON)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	st := NewStore(cfg)
	if _, err := st.SetLimit(TargetKey, "vip-client", 80); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Lookup("vip-client").Limit != 80 || loaded.Lookup("pro:eu:x").Limit != 250 {
		t.Fatalf("vip=%+v eu=%+v", loaded.Lookup("vip-client"), loaded.Lookup("pro:eu:x"))
	}
	rules := st.List()
	if len(rules) != 4 || rules[0].Target != TargetDefault || rules[1].Name != "vip-client" || rules[2].Name != "pro:" || rules[3].Name != "pro:eu:" {
		t.Fatalf("list: %+v", rules)
	}
}

func TestSetLimitSaveFailureKeepsSnapshot(t *testing.T) {
	path := writeTemp(t, "check.yaml", sampleYAML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.path = filepath.Join(t.TempDir(), "missing", "check.yaml")
	st := NewStore(cfg)
	if _, err := st.SetLimit(TargetDefault, "", 7); err == nil {
		t.Fatal("expected save error")
	}
	if st.Lookup("other").Limit != 100 {
		t.Fatalf("limit=%d", st.Lookup("other").Limit)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "limit: 100") {
		t.Fatalf("original file changed: %s", body)
	}
}

func TestSetLimitConcurrentLookup(t *testing.T) {
	cfg, err := Load(writeTemp(t, "check.yaml", sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	st := NewStore(cfg)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			if _, err := st.SetLimit(TargetDefault, "", int64(100+i)); err != nil {
				t.Errorf("set: %v", err)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			p := st.Lookup("other")
			if p.Limit <= 0 || p.Window != time.Minute || p.Fail != FailClosed {
				t.Errorf("lookup %+v", p)
			}
		}
	}()
	wg.Wait()
}
