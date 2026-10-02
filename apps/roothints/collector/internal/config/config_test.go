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
	if c.SourceURL != "https://www.internic.net/domain/named.root" ||
		c.MD5URL != "https://www.internic.net/domain/named.root.md5" {
		t.Errorf("urls = %q / %q", c.SourceURL, c.MD5URL)
	}
	if c.SyncInterval != time.Hour || c.RetryInterval != 5*time.Minute || c.RunTimeout != 10*time.Minute ||
		c.MinServers != 13 || c.RemovalThreshold != 0.05 || c.Once || c.Force ||
		c.LogLevel != "info" || c.LogFormat != "json" || c.UserAgent != "" {
		t.Errorf("c = %+v", c)
	}
}

func TestPrecedence(t *testing.T) {
	c, err := Load([]string{"--sync-interval", "5m"},
		env(map[string]string{"POSTGRES_URL": "postgres://x", "SYNC_INTERVAL": "2h", "MIN_SERVERS": "10"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.SyncInterval != 5*time.Minute {
		t.Errorf("argumento deveria vencer o ambiente: %v", c.SyncInterval)
	}
	if c.MinServers != 10 {
		t.Errorf("ambiente deveria vencer o padrão: %d", c.MinServers)
	}
}

func TestAllFromEnv(t *testing.T) {
	c, err := Load(nil, env(map[string]string{
		"POSTGRES_URL": "postgres://x", "SOURCE_URL": "http://mirror/named.root", "RETRY_INTERVAL": "1m",
		"RUN_TIMEOUT": "2m", "REMOVAL_THRESHOLD": "0.2", "USER_AGENT": "ua", "LOG_LEVEL": "DEBUG", "LOG_FORMAT": "text",
	}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.SourceURL != "http://mirror/named.root" || c.MD5URL != "http://mirror/named.root.md5" || c.RetryInterval != time.Minute || c.RunTimeout != 2*time.Minute ||
		c.RemovalThreshold != 0.2 || c.UserAgent != "ua" || c.LogLevel != "debug" || c.LogFormat != "text" {
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

func TestMD5URL(t *testing.T) {
	cases := map[string]string{
		"off":                   "",
		"OFF":                   "",
		"none":                  "",
		"false":                 "",
		"https://espelho/x.md5": "https://espelho/x.md5",
		"":                      "https://outro/named.root.md5",
	}
	for in, want := range cases {
		c, err := Load(nil, env(map[string]string{"POSTGRES_URL": "postgres://x", "SOURCE_URL": "https://outro/named.root", "SOURCE_MD5_URL": in}), io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if c.MD5URL != want {
			t.Errorf("SOURCE_MD5_URL=%q: MD5URL = %q, quero %q", in, c.MD5URL, want)
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
		{"POSTGRES_URL": "x", "REMOVAL_THRESHOLD": "2"},
		{"POSTGRES_URL": "x", "SOURCE_URL": "ftp://x"},
		{"POSTGRES_URL": "x", "LOG_LEVEL": "trace"},
		{"POSTGRES_URL": "x", "LOG_FORMAT": "xml"},
		{"POSTGRES_URL": "x", "MIN_SERVERS": "-1"},
		{"POSTGRES_URL": "x", "MIN_SERVERS": "muitos"},
		{"POSTGRES_URL": "x", "SOURCE_MD5_URL": "ftp://x.md5"},
		{"POSTGRES_URL": "x", "RUN_TIMEOUT": "10"},
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
