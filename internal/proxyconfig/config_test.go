package proxyconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dgallantino/api-rate-limiter/pkg/httplimit"
)

const sample = `
listen_addr: ":8080"
origin_url: "http://127.0.0.1:8000"
check_addr: "127.0.0.1:50051"
key: "header:X-API-Key"
cost: 1
fail: closed
`

func TestLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "proxy.yaml")
	if err := os.WriteFile(p, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":8080" || cfg.CheckAddr != "127.0.0.1:50051" {
		t.Fatalf("listen/check: %+v", cfg)
	}
	if cfg.OriginURL.String() != "http://127.0.0.1:8000" {
		t.Fatalf("origin: %s", cfg.OriginURL)
	}
	if cfg.Cost != 1 || cfg.Fail != httplimit.FailClosed {
		t.Fatalf("cost/fail: %+v", cfg)
	}
}

func TestLoadDefaultsAndPeek(t *testing.T) {
	p := filepath.Join(t.TempDir(), "proxy.yaml")
	body := `
listen_addr: ":8080"
origin_url: "http://127.0.0.1:8000"
check_addr: "127.0.0.1:50051"
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cost != 1 || cfg.Fail != httplimit.FailClosed || cfg.KeySpec != "header:X-API-Key" {
		t.Fatalf("defaults: %+v", cfg)
	}

	body = `
listen_addr: ":8080"
origin_url: "http://127.0.0.1:8000"
check_addr: "127.0.0.1:50051"
cost: 0
fail: open
key: ip
`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cost != 0 || cfg.Fail != httplimit.FailOpen || cfg.KeySpec != "ip" {
		t.Fatalf("peek/open/ip: %+v", cfg)
	}
}

func TestLoadValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "proxy.yaml")
	if err := os.WriteFile(p, []byte("listen_addr: \":1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("expected error")
	}
}
