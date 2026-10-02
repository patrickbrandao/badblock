package config

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/source"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDefaults(t *testing.T) {
	c, err := Load(nil, env(map[string]string{"POSTGRES_URL": "postgres://x"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.IANABaseURL != source.DefaultIANABaseURL || c.RDAPBaseURL != source.DefaultRDAPBaseURL {
		t.Errorf("bases = %q / %q", c.IANABaseURL, c.RDAPBaseURL)
	}
	if c.SyncInterval != 6*time.Hour || c.RetryInterval != 5*time.Minute || c.RunTimeout != 10*time.Minute ||
		c.RemovalThreshold != 0.05 || c.Once || c.Force || c.LogFormat != "json" || c.LogLevel != "info" {
		t.Errorf("c = %+v", c)
	}
}

func TestPrecedence(t *testing.T) {
	c, err := Load([]string{"--sync-interval", "5m"},
		env(map[string]string{"POSTGRES_URL": "postgres://x", "SYNC_INTERVAL": "2h", "REMOVAL_THRESHOLD": "0.2"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.SyncInterval != 5*time.Minute {
		t.Errorf("argumento deveria vencer o ambiente: %v", c.SyncInterval)
	}
	if c.RemovalThreshold != 0.2 {
		t.Errorf("ambiente deveria vencer o padrão: %v", c.RemovalThreshold)
	}
}

func TestBaseURLs(t *testing.T) {
	c, err := Load([]string{"--rdap-base-url", "http://127.0.0.1:9/rdap/"},
		env(map[string]string{"POSTGRES_URL": "postgres://x", "IANA_BASE_URL": "http://127.0.0.1:9/assignments/"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// A barra final sai: os caminhos fixos são concatenados com "/".
	if c.IANABaseURL != "http://127.0.0.1:9/assignments" || c.RDAPBaseURL != "http://127.0.0.1:9/rdap" {
		t.Errorf("bases = %q / %q", c.IANABaseURL, c.RDAPBaseURL)
	}
	if got := source.Files[0].URL(c.IANABaseURL, c.RDAPBaseURL); got != "http://127.0.0.1:9/assignments/as-numbers/as-numbers-1.csv" {
		t.Errorf("URL = %q", got)
	}
	if got := source.Files[9].URL(c.IANABaseURL, c.RDAPBaseURL); got != "http://127.0.0.1:9/rdap/ipv6.json" {
		t.Errorf("URL = %q", got)
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

func TestVersionWithoutPostgres(t *testing.T) {
	if _, err := Load([]string{"--version"}, env(nil), io.Discard); err != nil {
		t.Errorf("--version não precisa de POSTGRES_URL: %v", err)
	}
}

func TestInvalid(t *testing.T) {
	cases := []map[string]string{
		{},
		{"POSTGRES_URL": "x", "SYNC_INTERVAL": "0s"},
		{"POSTGRES_URL": "x", "RUN_TIMEOUT": "dez"},
		{"POSTGRES_URL": "x", "REMOVAL_THRESHOLD": "2"},
		{"POSTGRES_URL": "x", "IANA_BASE_URL": "ftp://x"},
		{"POSTGRES_URL": "x", "RDAP_BASE_URL": "data.iana.org/rdap"},
		{"POSTGRES_URL": "x", "LOG_LEVEL": "trace"},
		{"POSTGRES_URL": "x", "LOG_FORMAT": "xml"},
	}
	for _, m := range cases {
		if _, err := Load(nil, env(m), io.Discard); err == nil {
			t.Errorf("esperava erro para %v", m)
		}
	}
	if _, err := Load([]string{"extra"}, env(map[string]string{"POSTGRES_URL": "x"}), io.Discard); err == nil {
		t.Error("esperava erro para argumento solto")
	}
}

func TestHelpListsEveryOption(t *testing.T) {
	var b bytes.Buffer
	PrintHelp(&b)
	for _, o := range options {
		if !strings.Contains(b.String(), "--"+o.flag) || !strings.Contains(b.String(), "env "+o.env) {
			t.Errorf("--help sem %s / %s", o.flag, o.env)
		}
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("postgres://u:segredo@h:5432/db"); got != "postgres://u:***@h:5432/db" {
		t.Errorf("Redact = %q", got)
	}
}
