// Package config lê a configuração do collector-iana. Cada opção assume, em
// ordem: o valor padrão, a variável de ambiente e o argumento de linha de
// comando.
package config

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/source"
)

// Config é a configuração completa.
type Config struct {
	PostgresURL      string
	IANABaseURL      string
	RDAPBaseURL      string
	SyncInterval     time.Duration
	RetryInterval    time.Duration
	RunTimeout       time.Duration
	RemovalThreshold float64
	UserAgent        string
	LogLevel         string
	LogFormat        string

	Once        bool
	Force       bool
	ShowVersion bool
}

type option struct {
	flag, env, def, usage string
}

var options = []option{
	{"postgres-url", "POSTGRES_URL", "", "URL do Postgres, ex.: postgres://postgres:senha@badblock-postgres:5432/badblock (obrigatória)"},
	{"iana-base-url", "IANA_BASE_URL", source.DefaultIANABaseURL, "base dos 7 CSVs da IANA (caminhos fixos abaixo dela, ex.: /as-numbers/as-numbers-1.csv)"},
	{"rdap-base-url", "RDAP_BASE_URL", source.DefaultRDAPBaseURL, "base dos 3 JSONs do bootstrap RDAP (/asn.json, /ipv4.json, /ipv6.json)"},
	{"sync-interval", "SYNC_INTERVAL", "6h", "intervalo entre verificações da fonte"},
	{"retry-interval", "RETRY_INTERVAL", "5m", "espera até a próxima tentativa depois de uma verificação que falhou"},
	{"run-timeout", "RUN_TIMEOUT", "10m", "tempo máximo de uma verificação (downloads + aplicação)"},
	{"removal-threshold", "REMOVAL_THRESHOLD", "0.05", "fração máxima de linhas removidas de cada tabela sem --force (remoções de até 2 linhas sempre passam)"},
	{"user-agent", "USER_AGENT", "", "User-Agent das requisições (vazio = badblock-collector-iana/<versão>)"},
	{"log-level", "LOG_LEVEL", "info", "debug, info, warn ou error"},
	{"log-format", "LOG_FORMAT", "json", "json ou text"},
}

// Load interpreta os argumentos (sem o nome do programa) e o ambiente.
func Load(args []string, getenv func(string) string, stderr io.Writer) (*Config, error) {
	fs := flag.NewFlagSet("collector-iana", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { PrintHelp(stderr) }
	values := map[string]*string{}
	for _, o := range options {
		def := o.def
		if v := getenv(o.env); v != "" {
			def = v
		}
		values[o.flag] = fs.String(o.flag, def, o.usage)
	}
	c := &Config{}
	fs.BoolVar(&c.Once, "once", false, "faz uma verificação e sai")
	fs.BoolVar(&c.Force, "force", false, "aplica mesmo sem mudança e ignora a trava de remoção (implica --once)")
	fs.BoolVar(&c.ShowVersion, "version", false, "mostra a versão e sai")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("argumento inesperado: %s", fs.Arg(0))
	}
	if c.Force {
		c.Once = true
	}

	var err error
	c.PostgresURL = *values["postgres-url"]
	c.IANABaseURL = strings.TrimRight(*values["iana-base-url"], "/")
	c.RDAPBaseURL = strings.TrimRight(*values["rdap-base-url"], "/")
	c.UserAgent = *values["user-agent"]
	c.LogLevel = strings.ToLower(*values["log-level"])
	c.LogFormat = strings.ToLower(*values["log-format"])

	for _, d := range []struct {
		flag string
		dst  *time.Duration
	}{{"sync-interval", &c.SyncInterval}, {"retry-interval", &c.RetryInterval}, {"run-timeout", &c.RunTimeout}} {
		if *d.dst, err = time.ParseDuration(*values[d.flag]); err != nil || *d.dst <= 0 {
			return nil, fmt.Errorf("--%s inválido: %q", d.flag, *values[d.flag])
		}
	}
	c.RemovalThreshold, err = strconv.ParseFloat(*values["removal-threshold"], 64)
	if err != nil || c.RemovalThreshold < 0 || c.RemovalThreshold > 1 {
		return nil, fmt.Errorf("--removal-threshold inválido (0 a 1): %q", *values["removal-threshold"])
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("--log-level inválido: %q", c.LogLevel)
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		return nil, fmt.Errorf("--log-format inválido: %q", c.LogFormat)
	}
	for _, u := range []struct{ flag, url string }{{"iana-base-url", c.IANABaseURL}, {"rdap-base-url", c.RDAPBaseURL}} {
		if !strings.HasPrefix(u.url, "http://") && !strings.HasPrefix(u.url, "https://") {
			return nil, fmt.Errorf("--%s precisa ser http(s): %q", u.flag, u.url)
		}
	}
	if !c.ShowVersion && c.PostgresURL == "" {
		return nil, fmt.Errorf("defina POSTGRES_URL (ou --postgres-url)")
	}
	return c, nil
}

// PrintHelp escreve o texto de ajuda.
func PrintHelp(w io.Writer) {
	fmt.Fprint(w, `collector-iana — importa os registros de numeração da IANA (blocos de ASN e de
IP por RIR, special-purpose e bootstrap RDAP; 10 arquivos tratados como um
dataset) para as tabelas iana_* do PostgreSQL do BadBlock.

Verifica os arquivos a cada SYNC_INTERVAL e só aplica quando algum muda
(GET condicional com ETag/Last-Modified e hash do conteúdo). Não tem API HTTP.

Uso:
  collector-iana [opções]

Opções (padrão → variável de ambiente → argumento):
`)
	sorted := append([]option(nil), options...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].flag < sorted[j].flag })
	for _, o := range sorted {
		def := o.def
		if def == "" {
			def = `""`
		}
		fmt.Fprintf(w, "  --%-20s %s\n  %22s env %s, padrão %s\n", o.flag, o.usage, "", o.env, def)
	}
	fmt.Fprint(w, `  --once                 faz uma verificação e sai
  --force                aplica mesmo sem mudança e ignora a trava de remoção (implica --once)
  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
`)
}

// Redact esconde a senha de uma URL para o log.
func Redact(u string) string {
	at := strings.LastIndex(u, "@")
	scheme := strings.Index(u, "://")
	if at < 0 || scheme < 0 || at < scheme {
		return u
	}
	if user, _, ok := strings.Cut(u[scheme+3:at], ":"); ok {
		return u[:scheme+3] + user + ":***" + u[at:]
	}
	return u
}
