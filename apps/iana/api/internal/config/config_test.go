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
	if c.BasePath != "/iana" || c.HTTPPort != 8080 || c.CacheTTL != time.Hour || c.CacheTimeout != 50*time.Millisecond {
		t.Errorf("c = %+v", c)
	}
}

func TestBasePathNormalized(t *testing.T) {
	for in, want := range map[string]string{"iana": "/iana", "/iana/": "/iana", "/x/y/": "/x/y"} {
		c, err := Load([]string{"--base-path", in}, env(map[string]string{"POSTGRES_URL": "x"}), io.Discard)
		if err != nil || c.BasePath != want {
			t.Errorf("%q → %q, err %v", in, c.BasePath, err)
		}
	}
}

func TestPrecedence(t *testing.T) {
	c, err := Load([]string{"--http-port", "9000"},
		env(map[string]string{"POSTGRES_URL": "x", "HTTP_PORT": "8500", "DB_POOL_MAX": "3"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPPort != 9000 || c.DBPoolMax != 3 {
		t.Errorf("c = %+v", c)
	}
}

func TestInvalid(t *testing.T) {
	cases := []map[string]string{
		{},
		{"POSTGRES_URL": "x", "BASE_PATH": "/"},
		{"POSTGRES_URL": "x", "HTTP_PORT": "0"},
		{"POSTGRES_URL": "x", "REDIS_KEY_TTL": "-1"},
		{"POSTGRES_URL": "x", "TRUSTED_PROXIES": "lixo"},
		{"POSTGRES_URL": "x", "LOG_FORMAT": "xml"},
	}
	for _, m := range cases {
		if _, err := Load(nil, env(m), io.Discard); err == nil {
			t.Errorf("esperava erro para %v", m)
		}
	}
}
