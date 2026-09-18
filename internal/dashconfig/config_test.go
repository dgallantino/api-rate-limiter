package dashconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dash.yaml")
	body := "listen_addr: \":8081\"\ncheck_addr: \"127.0.0.1:50051\"\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":8081" || cfg.CheckAddr != "127.0.0.1:50051" {
		t.Fatalf("%+v", cfg)
	}
}

func TestLoadValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dash.yaml")
	if err := os.WriteFile(p, []byte("listen_addr: \":1\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("expected error")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "dash.json")); err == nil {
		t.Fatal("expected unsupported ext")
	}
}
