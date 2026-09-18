package dashconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr string
	CheckAddr  string
}

type file struct {
	ListenAddr string `yaml:"listen_addr"`
	CheckAddr  string `yaml:"check_addr"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".yaml" && ext != ".yml" {
		return nil, fmt.Errorf("dashconfig: unsupported extension %q", filepath.Ext(path))
	}
	var raw file
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw.toConfig()
}

func (f file) toConfig() (*Config, error) {
	if strings.TrimSpace(f.ListenAddr) == "" {
		return nil, fmt.Errorf("dashconfig: listen_addr is required")
	}
	if strings.TrimSpace(f.CheckAddr) == "" {
		return nil, fmt.Errorf("dashconfig: check_addr is required")
	}
	return &Config{
		ListenAddr: strings.TrimSpace(f.ListenAddr),
		CheckAddr:  strings.TrimSpace(f.CheckAddr),
	}, nil
}
