package config

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestPrecedenceAndValidation(t *testing.T) {
	if _, err := Load(nil, env(nil), &bytes.Buffer{}); err == nil {
		t.Fatal("sem POSTGRES_URL deveria falhar")
	}
	c, err := Load([]string{"--http-port=9000"}, env(map[string]string{
		"POSTGRES_URL": "postgres://x", "HTTP_PORT": "8100", "REDIS_TIMEOUT": "80ms", "REDIS_KEY_TTL": "60",
	}), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPPort != 9000 || c.CacheTimeout != 80*time.Millisecond || c.CacheTTL != time.Minute || !c.CacheEnabled {
		t.Errorf("config = %+v", c)
	}
	for _, args := range [][]string{
		{"--trusted-proxies=10.0.0.0/33"}, {"--real-ip-headers=X-Foo"}, {"--redis-key-ttl=0"},
		{"--db-timeout=abc"}, {"--redis-cache-enabled=talvez"},
	} {
		if _, err := Load(args, env(map[string]string{"POSTGRES_URL": "postgres://x"}), &bytes.Buffer{}); err == nil {
			t.Errorf("%v deveria falhar", args)
		}
	}
}

func TestHelpAndRedact(t *testing.T) {
	var buf bytes.Buffer
	PrintHelp(&buf)
	for _, o := range options {
		if !strings.Contains(buf.String(), o.env) {
			t.Errorf("ajuda sem %s", o.env)
		}
	}
	if got := Redact("redis://:segredo@badblock-valkey:6379/0"); strings.Contains(got, "segredo") {
		t.Errorf("Redact = %s", got)
	}
	if got := Redact("postgres://user:segredo@host/db"); got != "postgres://user:***@host/db" {
		t.Errorf("Redact = %s", got)
	}
}
