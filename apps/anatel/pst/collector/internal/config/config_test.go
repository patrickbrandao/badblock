package config

import (
	"bytes"
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
	if c.SourceURL != DefaultSourceURL {
		t.Errorf("url = %q", c.SourceURL)
	}
	if c.SyncInterval != 24*time.Hour || c.RetryInterval != 5*time.Minute || c.RunTimeout != 10*time.Minute ||
		c.MinProviders != 30000 || c.RemovalThreshold != 0.05 || c.Once || c.LogLevel != "info" || c.LogFormat != "json" {
		t.Errorf("c = %+v", c)
	}
}

func TestPrecedence(t *testing.T) {
	c, err := Load([]string{"--sync-interval", "5m"},
		env(map[string]string{"POSTGRES_URL": "postgres://x", "SYNC_INTERVAL": "2h", "MIN_PROVIDERS": "10", "LOG_LEVEL": "DEBUG"}), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if c.SyncInterval != 5*time.Minute {
		t.Errorf("argumento deveria vencer o ambiente: %v", c.SyncInterval)
	}
	if c.MinProviders != 10 {
		t.Errorf("ambiente deveria vencer o padrão: %d", c.MinProviders)
	}
	if c.LogLevel != "debug" {
		t.Errorf("LOG_LEVEL em qualquer caixa: %q", c.LogLevel)
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
	c, err := Load([]string{"--version"}, env(nil), io.Discard)
	if err != nil || !c.ShowVersion {
		t.Fatalf("c = %+v, err = %v", c, err)
	}
}

func TestInvalid(t *testing.T) {
	cases := []struct {
		env  map[string]string
		args []string
		msg  string
	}{
		{map[string]string{}, nil, "defina POSTGRES_URL (ou --postgres-url)"},
		{map[string]string{"POSTGRES_URL": "x", "SYNC_INTERVAL": "0s"}, nil, `--sync-interval inválido: "0s"`},
		{map[string]string{"POSTGRES_URL": "x", "RETRY_INTERVAL": "1d"}, nil, `--retry-interval inválido: "1d"`},
		{map[string]string{"POSTGRES_URL": "x", "RUN_TIMEOUT": "-1m"}, nil, `--run-timeout inválido: "-1m"`},
		{map[string]string{"POSTGRES_URL": "x", "MIN_PROVIDERS": "-1"}, nil, `--min-providers inválido: "-1"`},
		{map[string]string{"POSTGRES_URL": "x", "REMOVAL_THRESHOLD": "2"}, nil, `--removal-threshold inválido (0 a 1): "2"`},
		{map[string]string{"POSTGRES_URL": "x", "SOURCE_URL": "ftp://x"}, nil, `--source-url precisa ser http(s): "ftp://x"`},
		{map[string]string{"POSTGRES_URL": "x", "LOG_LEVEL": "trace"}, nil, `--log-level inválido: "trace"`},
		{map[string]string{"POSTGRES_URL": "x", "LOG_FORMAT": "xml"}, nil, `--log-format inválido: "xml"`},
		{map[string]string{"POSTGRES_URL": "x"}, []string{"solto"}, "argumento inesperado: solto"},
	}
	for _, tc := range cases {
		_, err := Load(tc.args, env(tc.env), io.Discard)
		if err == nil || err.Error() != tc.msg {
			t.Errorf("%v %v: err = %v, quero %q", tc.env, tc.args, err, tc.msg)
		}
	}
}

func TestHelp(t *testing.T) {
	var buf bytes.Buffer
	_, err := Load([]string{"--help"}, env(nil), &buf)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"collector-anatel-pst", "env MIN_PROVIDERS, padrão 30000", "env SYNC_INTERVAL, padrão 24h", "--force"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("ajuda sem %q", want)
		}
	}
}

func TestRedact(t *testing.T) {
	if got := Redact("postgres://u:segredo@h:5432/db"); got != "postgres://u:***@h:5432/db" {
		t.Errorf("Redact = %q", got)
	}
}
