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
	if c.BasePath != "/rdns" || c.HTTPPort != 8080 || c.CacheTTL != time.Hour || c.CacheTimeout != 50*time.Millisecond {
		t.Errorf("c = %+v", c)
	}
	if c.LiveTimeout != 5*time.Second || c.LiveMinAge != time.Minute || c.LiveQueueMax != 1000 {
		t.Errorf("live = %v %v %d", c.LiveTimeout, c.LiveMinAge, c.LiveQueueMax)
	}
}

func TestLiveOptions(t *testing.T) {
	cases := map[string]map[string]string{
		`--live-timeout inválido (1s a 25s): "500ms"`: {"POSTGRES_URL": "x", "LIVE_TIMEOUT": "500ms"},
		`--live-timeout inválido (1s a 25s): "26s"`:   {"POSTGRES_URL": "x", "LIVE_TIMEOUT": "26s"},
		`--live-min-age inválido: "0s"`:               {"POSTGRES_URL": "x", "LIVE_MIN_AGE": "0s"},
		`--live-queue-max inválido: "0"`:              {"POSTGRES_URL": "x", "LIVE_QUEUE_MAX": "0"},
	}
	for want, m := range cases {
		if _, err := Load(nil, env(m), io.Discard); err == nil || err.Error() != want {
			t.Errorf("%v: err = %v, quero %q", m, err, want)
		}
	}
	c, err := Load([]string{"--live-timeout", "25s"}, env(map[string]string{"POSTGRES_URL": "x", "LIVE_QUEUE_MAX": "5"}), io.Discard)
	if err != nil || c.LiveTimeout != 25*time.Second || c.LiveQueueMax != 5 {
		t.Errorf("c = %+v, %v", c, err)
	}
}

func TestBasePathNormalized(t *testing.T) {
	for in, want := range map[string]string{"rdns": "/rdns", "/rdns/": "/rdns", "/x/y/": "/x/y"} {
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
