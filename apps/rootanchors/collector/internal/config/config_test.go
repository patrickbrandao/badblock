package config

import (
	"errors"
	"flag"
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
	if c.SourceURL != "https://data.iana.org/root-anchors/root-anchors.xml" ||
		c.SHA256URL != "https://data.iana.org/root-anchors/checksums-sha256.txt" || c.SHA256Name != "root-anchors.xml" {
		t.Errorf("urls = %q / %q / %q", c.SourceURL, c.SHA256URL, c.SHA256Name)
	}
	if c.SyncInterval != 6*time.Hour || c.RetryInterval != 5*time.Minute || c.RunTimeout != 10*time.Minute ||
		c.MinKeys != 1 || c.RemovalThreshold != 0.05 || c.Once || c.Force ||
		c.LogLevel != "info" || c.LogFormat != "json" || c.UserAgent != "" {
		t.Errorf("c = %+v", c)
	}
}

func TestSHA256URLFollowsSource(t *testing.T) {
	c, err := Load([]string{"--source-url", "http://mirror.test/dns/anchors/root-anchors.xml?x=1"},
		env(map[string]string{"POSTGRES_URL": "postgres://x"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.SHA256URL != "http://mirror.test/dns/anchors/checksums-sha256.txt" || c.SHA256Name != "root-anchors.xml" {
		t.Errorf("sha = %q / %q", c.SHA256URL, c.SHA256Name)
	}
	c, err = Load(nil, env(map[string]string{"POSTGRES_URL": "postgres://x",
		"SOURCE_SHA256_URL": "https://h.test/sums.txt"}), io.Discard)
	if err != nil || c.SHA256URL != "https://h.test/sums.txt" || c.SHA256Name != "root-anchors.xml" {
		t.Errorf("c = %+v, err = %v", c, err)
	}
}

func TestSHA256Off(t *testing.T) {
	for _, v := range []string{"off", "NONE", "false"} {
		c, err := Load(nil, env(map[string]string{"POSTGRES_URL": "postgres://x", "SOURCE_SHA256_URL": v}), io.Discard)
		if err != nil || c.SHA256URL != "" {
			t.Errorf("%s: SHA256URL = %q, err = %v", v, c.SHA256URL, err)
		}
	}
}

func TestPrecedence(t *testing.T) {
	c, err := Load([]string{"--sync-interval", "5m"},
		env(map[string]string{"POSTGRES_URL": "postgres://x", "SYNC_INTERVAL": "2h", "MIN_KEYS": "2"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.SyncInterval != 5*time.Minute {
		t.Errorf("argumento deveria vencer o ambiente: %v", c.SyncInterval)
	}
	if c.MinKeys != 2 {
		t.Errorf("ambiente deveria vencer o padrão: %d", c.MinKeys)
	}
}

func TestAllFromEnv(t *testing.T) {
	c, err := Load(nil, env(map[string]string{
		"POSTGRES_URL": "postgres://x", "SOURCE_URL": "http://mirror/root-anchors.xml", "RETRY_INTERVAL": "1m",
		"RUN_TIMEOUT": "2m", "REMOVAL_THRESHOLD": "0.5", "USER_AGENT": "ua", "LOG_LEVEL": "DEBUG", "LOG_FORMAT": "text",
	}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.SourceURL != "http://mirror/root-anchors.xml" || c.RetryInterval != time.Minute || c.RunTimeout != 2*time.Minute ||
		c.RemovalThreshold != 0.5 || c.UserAgent != "ua" || c.LogLevel != "debug" || c.LogFormat != "text" {
		t.Errorf("c = %+v", c)
	}
}

func TestVersionWithoutPostgres(t *testing.T) {
	c, err := Load([]string{"--version"}, env(nil), io.Discard)
	if err != nil || !c.ShowVersion {
		t.Fatalf("--version não exige POSTGRES_URL: %+v, %v", c, err)
	}
}

func TestHelpListsEveryOption(t *testing.T) {
	var b strings.Builder
	if _, err := Load([]string{"--help"}, env(nil), &b); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v", err)
	}
	for _, o := range options {
		if !strings.Contains(b.String(), "--"+o.flag) || !strings.Contains(b.String(), o.env) {
			t.Errorf("--help sem %s/%s", o.flag, o.env)
		}
	}
	for _, f := range []string{"--once", "--force", "--version"} {
		if !strings.Contains(b.String(), f) {
			t.Errorf("--help sem %s", f)
		}
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
		{"POSTGRES_URL": "x", "SYNC_INTERVAL": "1d"},
		{"POSTGRES_URL": "x", "RUN_TIMEOUT": "10"},
		{"POSTGRES_URL": "x", "REMOVAL_THRESHOLD": "2"},
		{"POSTGRES_URL": "x", "SOURCE_URL": "ftp://x/root-anchors.xml"},
		{"POSTGRES_URL": "x", "SOURCE_URL": "https://x/"},
		{"POSTGRES_URL": "x", "SOURCE_SHA256_URL": "ftp://x/sums"},
		{"POSTGRES_URL": "x", "LOG_LEVEL": "trace"},
		{"POSTGRES_URL": "x", "LOG_FORMAT": "xml"},
		{"POSTGRES_URL": "x", "MIN_KEYS": "-1"},
		{"POSTGRES_URL": "x", "MIN_KEYS": "muitas"},
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

func TestUnexpectedArgument(t *testing.T) {
	if _, err := Load([]string{"extra"}, env(map[string]string{"POSTGRES_URL": "x"}), io.Discard); err == nil {
		t.Error("esperava erro para argumento posicional")
	}
}
