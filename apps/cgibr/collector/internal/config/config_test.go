package config

import (
	"io"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c, err := Load(nil, env(map[string]string{"POSTGRES_URL": "postgres://x"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.SourceURL != DefaultSourceURL || c.SHA256URL != DefaultSourceURL+".sha256" {
		t.Errorf("urls = %q / %q", c.SourceURL, c.SHA256URL)
	}
	if c.SyncInterval != time.Hour || c.RetryInterval != 5*time.Minute || c.MinASNs != 5000 || c.RemovalThreshold != 0.05 || c.Once {
		t.Errorf("c = %+v", c)
	}
}

func TestPrecedence(t *testing.T) {
	c, err := Load([]string{"--sync-interval", "5m"},
		env(map[string]string{"POSTGRES_URL": "postgres://x", "SYNC_INTERVAL": "2h", "MIN_ASNS": "10"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.SyncInterval != 5*time.Minute {
		t.Errorf("argumento deveria vencer o ambiente: %v", c.SyncInterval)
	}
	if c.MinASNs != 10 {
		t.Errorf("ambiente deveria vencer o padrão: %d", c.MinASNs)
	}
}

func TestSHA256Off(t *testing.T) {
	c, err := Load(nil, env(map[string]string{"POSTGRES_URL": "postgres://x", "SOURCE_SHA256_URL": "off"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.SHA256URL != "" {
		t.Errorf("SHA256URL = %q", c.SHA256URL)
	}
}

func TestForceImpliesOnce(t *testing.T) {
	c, err := Load([]string{"--force"}, env(map[string]string{"POSTGRES_URL": "postgres://x"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Once {
		t.Error("--force deveria implicar --once")
	}
}

func TestInvalid(t *testing.T) {
	cases := []map[string]string{
		{},
		{"POSTGRES_URL": "x", "SYNC_INTERVAL": "0s"},
		{"POSTGRES_URL": "x", "REMOVAL_THRESHOLD": "2"},
		{"POSTGRES_URL": "x", "SOURCE_URL": "ftp://x"},
		{"POSTGRES_URL": "x", "LOG_LEVEL": "trace"},
	}
	for _, m := range cases {
		if _, err := Load(nil, env(m), io.Discard); err == nil {
			t.Errorf("esperava erro para %v", m)
		}
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("postgres://u:segredo@h:5432/db"); got != "postgres://u:***@h:5432/db" {
		t.Errorf("Redact = %q", got)
	}
}
