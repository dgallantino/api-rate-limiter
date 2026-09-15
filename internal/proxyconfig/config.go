package proxyconfig

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/dgallantino/api-rate-limiter/pkg/httplimit"
	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr string
	OriginURL  *url.URL
	CheckAddr  string
	KeyFunc    httplimit.KeyFunc
	KeySpec    string
	Cost       int64
	Fail       httplimit.FailMode
}

type file struct {
	ListenAddr string `yaml:"listen_addr"`
	OriginURL  string `yaml:"origin_url"`
	CheckAddr  string `yaml:"check_addr"`
	Key        string `yaml:"key"`
	Cost       *int64 `yaml:"cost"`
	Fail       string `yaml:"fail"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".yaml" && ext != ".yml" {
		return nil, fmt.Errorf("proxyconfig: unsupported extension %q", filepath.Ext(path))
	}
	var raw file
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw.toConfig()
}

func (f file) toConfig() (*Config, error) {
	if strings.TrimSpace(f.ListenAddr) == "" {
		return nil, fmt.Errorf("proxyconfig: listen_addr is required")
	}
	if strings.TrimSpace(f.CheckAddr) == "" {
		return nil, fmt.Errorf("proxyconfig: check_addr is required")
	}
	origin, err := url.Parse(strings.TrimSpace(f.OriginURL))
	if err != nil || origin.Scheme == "" || origin.Host == "" {
		return nil, fmt.Errorf("proxyconfig: origin_url must be an absolute URL")
	}
	keyFn, err := httplimit.ParseKeySource(f.Key)
	if err != nil {
		return nil, fmt.Errorf("proxyconfig: key: %w", err)
	}
	fail, err := httplimit.ParseFail(f.Fail)
	if err != nil {
		return nil, fmt.Errorf("proxyconfig: fail: %w", err)
	}
	cost := int64(1)
	if f.Cost != nil {
		cost = *f.Cost
	}
	if cost < 0 {
		return nil, fmt.Errorf("proxyconfig: cost must be >= 0")
	}
	keySpec := strings.TrimSpace(f.Key)
	if keySpec == "" {
		keySpec = "header:X-API-Key"
	}
	return &Config{
		ListenAddr: f.ListenAddr,
		OriginURL:  origin,
		CheckAddr:  f.CheckAddr,
		KeyFunc:    keyFn,
		KeySpec:    keySpec,
		Cost:       cost,
		Fail:       fail,
	}, nil
}
