package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	ErrInvalidLimit  = errors.New("limit must be > 0")
	ErrUnknownPolicy = errors.New("unknown policy")
)

type Target string

const (
	TargetDefault Target = "default"
	TargetKey     Target = "key"
	TargetPrefix  Target = "prefix"
)

type Rule struct {
	Target Target
	Name   string
	Policy Policy
}

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
	window string
}

func (p Policy) WindowString() string {
	if p.window != "" {
		return p.window
	}
	if p.Window == 0 {
		return "0s"
	}
	return p.Window.String()
}

type Config struct {
	ListenAddr  string
	MetricsAddr string
	Redis       Redis
	Default     Policy
	keys        map[string]Policy
	prefixes    []prefixRule
	path        string
}

type prefixRule struct {
	prefix string
	policy Policy
}

type file struct {
	ListenAddr  string                `json:"listen_addr" yaml:"listen_addr"`
	MetricsAddr string                `json:"metrics_addr,omitempty" yaml:"metrics_addr,omitempty"`
	Redis       Redis                 `json:"redis" yaml:"redis"`
	Default     *policyFile           `json:"default" yaml:"default"`
	Keys        map[string]policyFile `json:"keys,omitempty" yaml:"keys,omitempty"`
	Prefixes    map[string]policyFile `json:"prefixes,omitempty" yaml:"prefixes,omitempty"`
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
	cfg, err := raw.toConfig()
	if err != nil {
		return nil, err
	}
	cfg.path = path
	return cfg, nil
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
		ListenAddr:  f.ListenAddr,
		MetricsAddr: strings.TrimSpace(f.MetricsAddr),
		Redis:       f.Redis,
		Default:     def,
		keys:        keys,
		prefixes:    prefixes,
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
	return Policy{Limit: p.Limit, Window: w, Fail: fail, window: p.Window}, nil
}

type Store struct {
	mu  sync.Mutex
	cur atomic.Pointer[Config]
}

func NewStore(cfg *Config) *Store {
	s := &Store{}
	if cfg == nil {
		cfg = &Config{}
	}
	s.cur.Store(cfg)
	return s
}

func (s *Store) Lookup(key string) Policy {
	return s.cur.Load().Lookup(key)
}

func (s *Store) List() []Rule {
	return s.cur.Load().list()
}

func (s *Store) SetLimit(target Target, name string, limit int64) (Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.cur.Load()
	next, rule, err := cur.withLimit(target, name, limit)
	if err != nil {
		return Rule{}, err
	}
	if cur.path != "" {
		if err := next.saveAtomic(); err != nil {
			return Rule{}, err
		}
	}
	s.cur.Store(next)
	return rule, nil
}

func (c *Config) list() []Rule {
	out := []Rule{{Target: TargetDefault, Policy: c.Default}}
	names := make([]string, 0, len(c.keys))
	for k := range c.keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, Rule{Target: TargetKey, Name: name, Policy: c.keys[name]})
	}
	prefs := append([]prefixRule(nil), c.prefixes...)
	sort.Slice(prefs, func(i, j int) bool { return prefs[i].prefix < prefs[j].prefix })
	for _, r := range prefs {
		out = append(out, Rule{Target: TargetPrefix, Name: r.prefix, Policy: r.policy})
	}
	return out
}

func (c *Config) clone() *Config {
	if c == nil {
		return &Config{}
	}
	next := *c
	next.keys = make(map[string]Policy, len(c.keys))
	for k, v := range c.keys {
		next.keys[k] = v
	}
	next.prefixes = append([]prefixRule(nil), c.prefixes...)
	return &next
}

func (c *Config) withLimit(target Target, name string, limit int64) (*Config, Rule, error) {
	if limit <= 0 {
		return nil, Rule{}, fmt.Errorf("config: %w", ErrInvalidLimit)
	}
	next := c.clone()
	switch target {
	case TargetDefault:
		if name != "" {
			return nil, Rule{}, fmt.Errorf("config: %w", ErrUnknownPolicy)
		}
		next.Default.Limit = limit
		return next, Rule{Target: TargetDefault, Policy: next.Default}, nil
	case TargetKey:
		p, ok := next.keys[name]
		if !ok {
			return nil, Rule{}, fmt.Errorf("config: %w", ErrUnknownPolicy)
		}
		p.Limit = limit
		next.keys[name] = p
		return next, Rule{Target: TargetKey, Name: name, Policy: p}, nil
	case TargetPrefix:
		for i := range next.prefixes {
			if next.prefixes[i].prefix != name {
				continue
			}
			next.prefixes[i].policy.Limit = limit
			return next, Rule{Target: TargetPrefix, Name: name, Policy: next.prefixes[i].policy}, nil
		}
		return nil, Rule{}, fmt.Errorf("config: %w", ErrUnknownPolicy)
	default:
		return nil, Rule{}, fmt.Errorf("config: %w", ErrUnknownPolicy)
	}
}

func (c *Config) saveAtomic() error {
	data, err := c.marshal()
	if err != nil {
		return err
	}
	dir := filepath.Dir(c.path)
	f, err := os.CreateTemp(dir, ".check-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmp)
		}
	}()
	if info, statErr := os.Stat(c.path); statErr == nil {
		if err := f.Chmod(info.Mode().Perm()); err != nil {
			_ = f.Close()
			return err
		}
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return err
	}
	renamed = true
	if dirf, err := os.Open(dir); err == nil {
		_ = dirf.Sync()
		_ = dirf.Close()
	}
	return nil
}

func (c *Config) marshal() ([]byte, error) {
	raw := c.toFile()
	switch strings.ToLower(filepath.Ext(c.path)) {
	case ".json":
		b, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	case ".yaml", ".yml":
		return yaml.Marshal(raw)
	default:
		return nil, fmt.Errorf("config: unsupported extension %q", filepath.Ext(c.path))
	}
}

func (c *Config) toFile() file {
	keys := make(map[string]policyFile, len(c.keys))
	for k, p := range c.keys {
		keys[k] = policyToFile(p)
	}
	prefixes := make(map[string]policyFile, len(c.prefixes))
	for _, r := range c.prefixes {
		prefixes[r.prefix] = policyToFile(r.policy)
	}
	def := policyToFile(c.Default)
	return file{
		ListenAddr:  c.ListenAddr,
		MetricsAddr: c.MetricsAddr,
		Redis:       c.Redis,
		Default:     &def,
		Keys:        keys,
		Prefixes:    prefixes,
	}
}

func policyToFile(p Policy) policyFile {
	return policyFile{Limit: p.Limit, Window: p.WindowString(), Fail: p.Fail.String()}
}
