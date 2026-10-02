package config

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/ripencc/collector/internal/rir"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c, err := Load(nil, env(map[string]string{"POSTGRES_URL": "postgres://x"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.SourceURL != rir.DefaultSourceURL || c.MD5URL != rir.DefaultSourceURL+".md5" {
		t.Errorf("urls = %q / %q", c.SourceURL, c.MD5URL)
	}
	if c.SyncInterval != time.Hour || c.RetryInterval != 5*time.Minute || c.RunTimeout != 10*time.Minute ||
		c.MinRecords != rir.DefaultMinRecords || c.RemovalThreshold != 0.05 || c.Once || c.Force {
		t.Errorf("c = %+v", c)
	}
	if c.LogLevel != "info" || c.LogFormat != "json" {
		t.Errorf("log = %s/%s", c.LogLevel, c.LogFormat)
	}
}

func TestPrecedence(t *testing.T) {
	c, err := Load([]string{"--sync-interval", "5m", "--min-records", "7"},
		env(map[string]string{"POSTGRES_URL": "postgres://x", "SYNC_INTERVAL": "2h", "MIN_RECORDS": "10", "RETRY_INTERVAL": "1m"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.SyncInterval != 5*time.Minute || c.MinRecords != 7 {
		t.Errorf("argumento deveria vencer o ambiente: %v / %d", c.SyncInterval, c.MinRecords)
	}
	if c.RetryInterval != time.Minute {
		t.Errorf("ambiente deveria vencer o padrão: %v", c.RetryInterval)
	}
}

// Variável vazia (como o compose passa quando o .env não define) vale o padrão.
func TestEmptyEnvUsesDefault(t *testing.T) {
	c, err := Load(nil, env(map[string]string{"POSTGRES_URL": "postgres://x", "SOURCE_URL": "", "MIN_RECORDS": ""}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.SourceURL != rir.DefaultSourceURL || c.MinRecords != rir.DefaultMinRecords {
		t.Errorf("c = %+v", c)
	}
}

func TestMD5URL(t *testing.T) {
	cases := map[string]string{
		"off":                   "",
		"OFF":                   "",
		"https://espelho/x.md5": "https://espelho/x.md5",
		"":                      "https://outro/arquivo.md5",
	}
	for in, want := range cases {
		c, err := Load(nil, env(map[string]string{"POSTGRES_URL": "postgres://x", "SOURCE_URL": "https://outro/arquivo", "SOURCE_MD5_URL": in}), io.Discard)
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
	if !c.Once || !c.Force {
		t.Error("--force deveria implicar --once")
	}
}

func TestVersionWithoutPostgres(t *testing.T) {
	c, err := Load([]string{"--version"}, env(nil), io.Discard)
	if err != nil || !c.ShowVersion {
		t.Errorf("c = %+v, err = %v", c, err)
	}
}

func TestHelp(t *testing.T) {
	var out bytes.Buffer
	_, err := Load([]string{"--help"}, env(nil), &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v", err)
	}
	for _, o := range options {
		if !strings.Contains(out.String(), "--"+o.flag) || !strings.Contains(out.String(), "env "+o.env) {
			t.Errorf("--help sem %s / %s", o.flag, o.env)
		}
	}
	if !strings.Contains(out.String(), rir.App) {
		t.Errorf("--help sem o nome do app")
	}
}

func TestInvalid(t *testing.T) {
	cases := []map[string]string{
		{},
		{"POSTGRES_URL": "x", "SYNC_INTERVAL": "0s"},
		{"POSTGRES_URL": "x", "RUN_TIMEOUT": "dez"},
		{"POSTGRES_URL": "x", "REMOVAL_THRESHOLD": "2"},
		{"POSTGRES_URL": "x", "MIN_RECORDS": "-1"},
		{"POSTGRES_URL": "x", "SOURCE_URL": "ftp://x"},
		{"POSTGRES_URL": "x", "SOURCE_MD5_URL": "ftp://x.md5"},
		{"POSTGRES_URL": "x", "LOG_LEVEL": "trace"},
		{"POSTGRES_URL": "x", "LOG_FORMAT": "xml"},
	}
	for _, m := range cases {
		if _, err := Load(nil, env(m), io.Discard); err == nil {
			t.Errorf("esperava erro para %v", m)
		}
	}
	if _, err := Load([]string{"sobra"}, env(map[string]string{"POSTGRES_URL": "x"}), io.Discard); err == nil {
		t.Error("esperava erro para argumento posicional")
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("postgres://u:segredo@h:5432/db"); got != "postgres://u:***@h:5432/db" {
		t.Errorf("Redact = %q", got)
	}
	if got := Redact("postgres://h/db"); got != "postgres://h/db" {
		t.Errorf("Redact sem senha = %q", got)
	}
}
