package config

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestPrecedence(t *testing.T) {
	// Sem nada: valores padrão (e erro por falta da URL).
	if _, err := Load(nil, env(nil), &bytes.Buffer{}); err == nil {
		t.Fatal("sem POSTGRES_URL deveria falhar")
	}

	// Ambiente sobrescreve o padrão.
	c, err := Load(nil, env(map[string]string{
		"POSTGRES_URL":  "postgres://env",
		"SYNC_INTERVAL": "30m",
	}), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if c.PostgresURL != "postgres://env" || c.SyncInterval != 30*time.Minute || c.HTTPPort != 8002 {
		t.Errorf("config = %+v", c)
	}

	// Argumento sobrescreve o ambiente.
	c, err = Load([]string{"--sync-interval=2h", "--once", "--source=rir-lacnic,nicbr"},
		env(map[string]string{"POSTGRES_URL": "postgres://env", "SYNC_INTERVAL": "30m"}), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if c.SyncInterval != 2*time.Hour || !c.Once || len(c.Sources) != 2 || c.Sources[1] != "nicbr" {
		t.Errorf("config = %+v", c)
	}
}

func TestInvalidValues(t *testing.T) {
	base := map[string]string{"POSTGRES_URL": "postgres://x"}
	for _, args := range [][]string{
		{"--removal-threshold=1.5"},
		{"--sync-interval=0s"},
		{"--source-intervals=nicbr"},
		{"--http-port=abc"},
		{"extra"},
	} {
		if _, err := Load(args, env(base), &bytes.Buffer{}); err == nil {
			t.Errorf("%v deveria falhar", args)
		}
	}
	c, err := Load([]string{"--source-intervals=iana-ipv4=6h, asnames=2h"}, env(base), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if c.SourceIntervals["iana-ipv4"] != 6*time.Hour || c.SourceIntervals["asnames"] != 2*time.Hour {
		t.Errorf("intervalos = %v", c.SourceIntervals)
	}
}

func TestHelpListsEnvVars(t *testing.T) {
	var buf bytes.Buffer
	PrintHelp(&buf)
	for _, o := range options {
		if !strings.Contains(buf.String(), o.env) || !strings.Contains(buf.String(), "--"+o.flag) {
			t.Errorf("ajuda sem %s / --%s", o.env, o.flag)
		}
	}
}
