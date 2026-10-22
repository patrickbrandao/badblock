package config

import (
	"bytes"
	"io"
	"strings"
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
	if c.BasePath != "/registrobr/rdapnum" || c.HTTPPort != 8080 || c.CacheTTL != 300*time.Second ||
		c.CacheTimeout != 50*time.Millisecond || c.DatasetPoll != 30*time.Second {
		t.Errorf("c = %+v", c)
	}
	if c.RefreshMinAge != 10*time.Minute || c.RefreshQueueMax != 1000 {
		t.Errorf("refresh = %v %d", c.RefreshMinAge, c.RefreshQueueMax)
	}
}

func TestRefreshOptions(t *testing.T) {
	cases := map[string]map[string]string{
		`--refresh-min-age inválido: "dez"`: {"POSTGRES_URL": "x", "REFRESH_MIN_AGE": "dez"},
		`--refresh-min-age inválido: "-1m"`: {"POSTGRES_URL": "x", "REFRESH_MIN_AGE": "-1m"},
		`--refresh-queue-max inválido: "0"`: {"POSTGRES_URL": "x", "REFRESH_QUEUE_MAX": "0"},
		`--refresh-queue-max inválido: "x"`: {"POSTGRES_URL": "x", "REFRESH_QUEUE_MAX": "x"},
	}
	for want, m := range cases {
		if _, err := Load(nil, env(m), io.Discard); err == nil || err.Error() != want {
			t.Errorf("%v: err = %v, quero %q", m, err, want)
		}
	}
	c, err := Load([]string{"--refresh-min-age", "1h"}, env(map[string]string{"POSTGRES_URL": "x", "REFRESH_QUEUE_MAX": "5"}), io.Discard)
	if err != nil || c.RefreshMinAge != time.Hour || c.RefreshQueueMax != 5 {
		t.Errorf("c = %+v, %v", c, err)
	}
}

func TestBasePathNormalized(t *testing.T) {
	for in, want := range map[string]string{"registrobr/rdapnum": "/registrobr/rdapnum", "/registrobr/rdapnum/": "/registrobr/rdapnum", "/x/": "/x"} {
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

func TestHelp(t *testing.T) {
	var b bytes.Buffer
	PrintHelp(&b)
	for _, s := range []string{"api-registrobr-rdapnum", "REFRESH_MIN_AGE", "REFRESH_QUEUE_MAX", "padrão 300"} {
		if !strings.Contains(b.String(), s) {
			t.Errorf("ajuda sem %q", s)
		}
	}
}
