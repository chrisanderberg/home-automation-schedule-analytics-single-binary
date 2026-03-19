package config

import "testing"

func TestLoadRejectsInvalidPort(t *testing.T) {
	t.Setenv("HAA_LATITUDE", "1")
	t.Setenv("HAA_LONGITUDE", "2")
	t.Setenv("HAA_PORT", "70000")

	_, err := Load()
	if err == nil {
		t.Fatal("expected invalid port error")
	}
}

func TestLoadAcceptsValidPort(t *testing.T) {
	t.Setenv("HAA_LATITUDE", "1")
	t.Setenv("HAA_LONGITUDE", "2")
	t.Setenv("HAA_PORT", "8081")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Port != "8081" {
		t.Fatalf("expected port 8081, got %q", cfg.Port)
	}
}
