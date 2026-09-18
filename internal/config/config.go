package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type FailMode int

const (
	FailClosed FailMode = iota
	FailOpen
)

func (f FailMode) String() string {
	if f == FailOpen {
		return "open"
	}
	return "closed"
}

type Redis struct {
	Addr string `json:"addr" yaml:"addr"`
}

type Policy struct {
	Limit  int64
	Window time.Duration
	Fail   FailMode
}

type Config struct {
	ListenAddr string
	Redis      Redis
	Default    Policy
	keys       map[string]Policy
	prefixes   []prefixRule
}

type prefixRule struct {
	prefix string
	policy Policy
}

type file struct {
	ListenAddr string                `json:"listen_addr" yaml:"listen_addr"`
	Redis      Redis                 `json:"redis" yaml:"redis"`
	Default    *policyFile           `json:"default" yaml:"default"`
	Keys       map[string]policyFile `json:"keys" yaml:"keys"`
	Prefixes   map[string]policyFile `json:"prefixes" yaml:"prefixes"`
}

type policyFile struct {
	Limit  int64  `json:"limit" yaml:"limit"`
	Window string `json:"window" yaml:"window"`
	Fail   string `json:"fail" yaml:"fail"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw file
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		err = json.Unmarshal(data, &raw)
	case ".yaml", ".yml":
		err = yaml.Unmarshal(data, &raw)
	default:
		return nil, fmt.Errorf("config: unsupported extension %q", filepath.Ext(path))
	}
	if err != nil {
		return nil, err
	}
	return raw.toConfig()
}

func (c *Config) Lookup(key string) Policy {
	if p, ok := c.keys[key]; ok {
		return p
	}
	bestLen := -1
	var best Policy
	for _, r := range c.prefixes {
		if len(r.prefix) > bestLen && strings.HasPrefix(key, r.prefix) {
			best = r.policy
			bestLen = len(r.prefix)
		}
	}
	if bestLen >= 0 {
		return best
	}
	return c.Default
}

func (f file) toConfig() (*Config, error) {
	if strings.TrimSpace(f.ListenAddr) == "" {
		return nil, fmt.Errorf("config: listen_addr is required")
	}
	if strings.TrimSpace(f.Redis.Addr) == "" {
		return nil, fmt.Errorf("config: redis.addr is required")
	}
	if f.Default == nil {
		return nil, fmt.Errorf("config: default policy is required")
	}
	def, err := parsePolicy(*f.Default, "default")
	if err != nil {
		return nil, err
	}
	keys := make(map[string]Policy, len(f.Keys))
	for k, p := range f.Keys {
		if k == "" {
			return nil, fmt.Errorf("config: empty key")
		}
		pol, err := parsePolicy(p, "keys["+k+"]")
		if err != nil {
			return nil, err
		}
		keys[k] = pol
	}
	prefixes := make([]prefixRule, 0, len(f.Prefixes))
	for pref, p := range f.Prefixes {
		if pref == "" {
			return nil, fmt.Errorf("config: empty prefix")
		}
		pol, err := parsePolicy(p, "prefixes["+pref+"]")
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, prefixRule{prefix: pref, policy: pol})
	}
	return &Config{
		ListenAddr: f.ListenAddr,
		Redis:      f.Redis,
		Default:    def,
		keys:       keys,
		prefixes:   prefixes,
	}, nil
}

func parsePolicy(p policyFile, what string) (Policy, error) {
	if p.Limit <= 0 {
		return Policy{}, fmt.Errorf("config: %s: limit must be > 0", what)
	}
	w, err := time.ParseDuration(p.Window)
	if err != nil {
		return Policy{}, fmt.Errorf("config: %s: window: %w", what, err)
	}
	if w <= 0 {
		return Policy{}, fmt.Errorf("config: %s: window must be > 0", what)
	}
	var fail FailMode
	switch strings.ToLower(strings.TrimSpace(p.Fail)) {
	case "closed":
		fail = FailClosed
	case "open":
		fail = FailOpen
	default:
		return Policy{}, fmt.Errorf("config: %s: fail must be open or closed", what)
	}
	return Policy{Limit: p.Limit, Window: w, Fail: fail}, nil
}
