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

func base() map[string]string {
	return map[string]string{"POSTGRES_URL": "postgres://x", "DNS_SERVERS": "1.1.1.1"}
}

func with(kv ...string) map[string]string {
	m := base()
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func TestDefaults(t *testing.T) {
	c, err := Load(nil, env(base()), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.DNSTimeout != 2*time.Second || c.DNSRetries != 2 || c.DNSInflight != 512 || c.RateLimit != 256 {
		t.Errorf("DNS = %+v", c)
	}
	if c.ScanInterval != 24*time.Hour || c.ScanLease != 2*time.Minute || c.DeriveInterval != 5*time.Minute || c.RunTimeout != 10*time.Minute {
		t.Errorf("intervalos = %+v", c)
	}
	if !slices.Equal(c.Sources, []string{"cgibr"}) || c.RedisURL != "" || c.Once || c.Force {
		t.Errorf("c = %+v", c)
	}
	if !slices.Equal(c.DNSServers, []string{"1.1.1.1:53"}) {
		t.Errorf("servidores = %v", c.DNSServers)
	}
}

func TestPrecedence(t *testing.T) {
	c, err := Load([]string{"--rate-limit", "64", "--dns-servers", "9.9.9.9"},
		env(with("RATE_LIMIT", "512", "SCAN_INTERVAL", "12h")), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.RateLimit != 64 {
		t.Errorf("argumento deveria vencer o ambiente: %d", c.RateLimit)
	}
	if c.ScanInterval != 12*time.Hour {
		t.Errorf("ambiente deveria vencer o padrão: %v", c.ScanInterval)
	}
	if !slices.Equal(c.DNSServers, []string{"9.9.9.9:53"}) {
		t.Errorf("servidores = %v", c.DNSServers)
	}
}

func TestServers(t *testing.T) {
	got, err := ParseServers(" 1.1.1.1, 8.8.8.8:5353 ,[2001:db8::1]:53,2001:db8::2,")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1.1.1.1:53", "8.8.8.8:5353", "[2001:db8::1]:53", "[2001:db8::2]:53"}
	if !slices.Equal(got, want) {
		t.Errorf("servidores = %v, quero %v", got, want)
	}
	for _, bad := range []string{"dns.google", "1.1.1.1:0", "1.1.1.1:99999", "300.1.1.1", "fe80::1%eth0"} {
		if _, err := ParseServers(bad); err == nil || !strings.Contains(err.Error(), "servidor DNS inválido") {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}

func TestDNSServersRequired(t *testing.T) {
	_, err := Load(nil, env(map[string]string{"POSTGRES_URL": "postgres://x"}), io.Discard)
	if err == nil || err.Error() != "defina DNS_SERVERS (ou --dns-servers)" {
		t.Errorf("err = %v", err)
	}
	// --version dispensa DNS_SERVERS e POSTGRES_URL.
	if _, err := Load([]string{"--version"}, env(nil), io.Discard); err != nil {
		t.Errorf("--version: %v", err)
	}
	// --once não dispensa.
	if _, err := Load([]string{"--once"}, env(map[string]string{"POSTGRES_URL": "postgres://x"}), io.Discard); err == nil {
		t.Error("--once sem DNS_SERVERS deveria falhar")
	}
}

func TestSources(t *testing.T) {
	c, err := Load(nil, env(with("SOURCES", "none")), io.Discard)
	if err != nil || len(c.Sources) != 0 {
		t.Errorf("none = %v, %v", c, err)
	}
	c, err = Load(nil, env(with("SOURCES", "CGIBR")), io.Discard)
	if err != nil || !slices.Equal(c.Sources, []string{"cgibr"}) {
		t.Errorf("CGIBR = %v, %v", c, err)
	}
	_, err = Load(nil, env(with("SOURCES", "lacnic")), io.Discard)
	if err == nil || err.Error() != `--sources inválido: "lacnic" (use cgibr ou none)` {
		t.Errorf("lacnic: err = %v", err)
	}
}

func TestScanLeaseMinimum(t *testing.T) {
	for _, v := range []string{"29s", "0s", "abc"} {
		_, err := Load(nil, env(with("SCAN_LEASE", v)), io.Discard)
		if err == nil || !strings.HasPrefix(err.Error(), "--scan-lease inválido (mínimo 30s)") {
			t.Errorf("%s: err = %v", v, err)
		}
	}
	c, err := Load([]string{"--scan-lease", "30s"}, env(base()), io.Discard)
	if err != nil || c.ScanLease != 30*time.Second {
		t.Errorf("30s = %v, %v", c, err)
	}
}

func TestForceImpliesOnce(t *testing.T) {
	c, err := Load([]string{"--force"}, env(base()), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Once {
		t.Error("--force deveria implicar --once")
	}
}

func TestInvalid(t *testing.T) {
	cases := map[string]map[string]string{
		`--dns-retries inválido (0 a 5): "6"`:     with("DNS_RETRIES", "6"),
		`--dns-retries inválido (0 a 5): "-1"`:    with("DNS_RETRIES", "-1"),
		`--dns-inflight inválido: "0"`:            with("DNS_INFLIGHT", "0"),
		`--rate-limit inválido: "0"`:              with("RATE_LIMIT", "0"),
		`--rate-limit inválido: "x"`:              with("RATE_LIMIT", "x"),
		`--dns-timeout inválido: "0s"`:            with("DNS_TIMEOUT", "0s"),
		`--scan-interval inválido: "1d"`:          with("SCAN_INTERVAL", "1d"),
		`--derive-interval inválido: "-1m"`:       with("DERIVE_INTERVAL", "-1m"),
		`--run-timeout inválido: "x"`:             with("RUN_TIMEOUT", "x"),
		`--log-level inválido: "trace"`:           with("LOG_LEVEL", "trace"),
		`--log-format inválido: "xml"`:            with("LOG_FORMAT", "xml"),
		`servidor DNS inválido "a.b"`:             with("DNS_SERVERS", "1.1.1.1,a.b"),
		`defina POSTGRES_URL (ou --postgres-url)`: {"DNS_SERVERS": "1.1.1.1"},
	}
	for want, m := range cases {
		_, err := Load(nil, env(m), io.Discard)
		if err == nil || err.Error() != want {
			t.Errorf("%v: err = %v, quero %q", m, err, want)
		}
	}
	if _, err := Load([]string{"solto"}, env(base()), io.Discard); err == nil || err.Error() != "argumento inesperado: solto" {
		t.Errorf("argumento solto: %v", err)
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("postgres://u:segredo@h:5432/db"); got != "postgres://u:***@h:5432/db" {
		t.Errorf("Redact = %q", got)
	}
	if got := Redact("redis://:segredo@h:6379/0"); got != "redis://:***@h:6379/0" {
		t.Errorf("Redact = %q", got)
	}
}
