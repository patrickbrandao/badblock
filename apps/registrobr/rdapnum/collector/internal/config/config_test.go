package config

import (
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func with(kv ...string) map[string]string {
	m := map[string]string{"POSTGRES_URL": "postgres://x"}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func TestDefaults(t *testing.T) {
	c, err := Load(nil, env(with()), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.RDAPURL != "https://rdap.registro.br" || c.ProxyURL != nil || c.HTTPTimeout != 20*time.Second {
		t.Errorf("RDAP = %+v", c)
	}
	if c.RateLimit != 32 || c.ScanInterval != 168*time.Hour || c.ScanLease != 2*time.Minute {
		t.Errorf("fila = %+v", c)
	}
	if c.TruncatedPauseAfter != 3 || c.TruncatedPause != 2*time.Minute || c.ProxySameIPWarn != 10 {
		t.Errorf("truncadas = %+v", c)
	}
	if c.DeriveInterval != 5*time.Minute || c.RunTimeout != 10*time.Minute || !slices.Equal(c.Sources, []string{"cgibr"}) ||
		c.RedisURL != "" || c.Once || c.Force {
		t.Errorf("c = %+v", c)
	}
}

func TestPrecedence(t *testing.T) {
	c, err := Load([]string{"--rate-limit", "64", "--rdap-url", "http://127.0.0.1:8080/"},
		env(with("RATE_LIMIT", "48", "SCAN_INTERVAL", "24h", "SOURCES", "none")), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.RateLimit != 64 || c.ScanInterval != 24*time.Hour || c.Sources != nil || c.RDAPURL != "http://127.0.0.1:8080" {
		t.Errorf("c = %+v", c)
	}
	c, err = Load([]string{"--force"}, env(with()), io.Discard)
	if err != nil || !c.Once {
		t.Errorf("--force sem --once: %+v %v", c, err)
	}
}

func TestInvalid(t *testing.T) {
	cases := []struct {
		env  map[string]string
		args []string
		want string
	}{
		{with("RDAP_URL", "ftp://x"), nil, `--rdap-url inválida: "ftp://x"`},
		{with("RATE_LIMIT", "0"), nil, `--rate-limit inválido: "0"`},
		{with("SCAN_LEASE", "20s"), nil, "--scan-lease inválido (mínimo 30s e maior que --http-timeout + 10s)"},
		{with("SCAN_LEASE", "30s"), nil, "--scan-lease inválido"}, // 30s <= 20s + 10s
		{with("SCAN_LEASE", "40s", "HTTP_TIMEOUT", "5s"), nil, ""},
		{with("TRUNCATED_PAUSE_AFTER", "0"), nil, `--truncated-pause-after inválido: "0"`},
		{with("PROXY_SAME_IP_WARN", "-1"), nil, `--proxy-same-ip-warn inválido: "-1"`},
		{with("PROXY_SAME_IP_WARN", "0"), nil, ""},
		{with("TRUNCATED_PAUSE", "x"), nil, `--truncated-pause inválido: "x"`},
		{with("SOURCES", "rir"), nil, "--sources inválido"},
		{map[string]string{}, nil, "defina POSTGRES_URL"},
		{with(), []string{"extra"}, "argumento inesperado"},
	}
	for _, c := range cases {
		_, err := Load(c.args, env(c.env), io.Discard)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%v: %v", c.env, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%v: err = %v, quero %q", c.env, err, c.want)
		}
	}
}

func TestProxyURL(t *testing.T) {
	c, err := Load(nil, env(with("PROXY_URL", "socks5://user:segredo@proxy.example:1080")), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.ProxyURL == nil || c.ProxyURL.Scheme != "socks5" {
		t.Fatalf("proxy = %v", c.ProxyURL)
	}
	if r := RedactProxy(c.ProxyURL); r != "socks5://user:xxxxx@proxy.example:1080" {
		t.Errorf("mascarado = %q", r)
	}
	if RedactProxy(nil) != "none" {
		t.Error("sem proxy")
	}
	for _, v := range []string{"ftp://user:segredo@x:21", "user:segredo@x:3128", "http://user:segredo@:3128", "://segredo"} {
		_, err := Load(nil, env(with("PROXY_URL", v)), io.Discard)
		if err == nil {
			t.Errorf("%q aceita", v)
			continue
		}
		if err.Error() != "--proxy-url inválida (use http, https ou socks5)" || strings.Contains(err.Error(), "segredo") {
			t.Errorf("%q: %v", v, err)
		}
	}
	for _, v := range []string{"http://p:3128", "https://u:p@p:443"} {
		if _, err := Load(nil, env(with("PROXY_URL", v)), io.Discard); err != nil {
			t.Errorf("%q: %v", v, err)
		}
	}
}

func TestRateWarning(t *testing.T) {
	if !RateNeedsProxy(32, false) || RateNeedsProxy(8, false) || RateNeedsProxy(32, true) {
		t.Error("RateNeedsProxy")
	}
}

func TestRedact(t *testing.T) {
	if r := Redact("postgres://postgres:pw@h:5432/db"); r != "postgres://postgres:***@h:5432/db" {
		t.Error(r)
	}
}

func TestUserAgent(t *testing.T) {
	if ua := UserAgent("1.2.3"); ua != "badblock-collector-registrobr-rdapnum/1.2.3 (+https://github.com/patrickbrandao/badblock)" {
		t.Error(ua)
	}
}
