// Package config lê a configuração do collector-cgibr. Cada opção assume, em
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
)

// DefaultSourceURL é o arquivo do NIC.br com ASNs, titulares e blocos.
const DefaultSourceURL = "https://ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt"

// Config é a configuração completa.
type Config struct {
	PostgresURL      string
	SourceURL        string
	SHA256URL        string // vazio = sem conferência do hash publicado
	SyncInterval     time.Duration
	RetryInterval    time.Duration
	RunTimeout       time.Duration
	MinASNs          int
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
	{"source-url", "SOURCE_URL", DefaultSourceURL, "arquivo nicbr-asn-blk do NIC.br"},
	{"source-sha256-url", "SOURCE_SHA256_URL", "", `hash publicado; vazio = SOURCE_URL + ".sha256", "off" desliga a conferência`},
	{"sync-interval", "SYNC_INTERVAL", "1h", "intervalo entre verificações da fonte"},
	{"retry-interval", "RETRY_INTERVAL", "5m", "espera até a próxima tentativa depois de uma verificação que falhou"},
	{"run-timeout", "RUN_TIMEOUT", "10m", "tempo máximo de uma verificação (download + aplicação)"},
	{"min-asns", "MIN_ASNS", "5000", "abaixo disso o arquivo é tratado como truncado e recusado"},
	{"removal-threshold", "REMOVAL_THRESHOLD", "0.05", "fração máxima de ASNs ou blocos removidos de uma vez sem --force"},
	{"user-agent", "USER_AGENT", "", "User-Agent das requisições (vazio = badblock-collector-cgibr/<versão>)"},
	{"log-level", "LOG_LEVEL", "info", "debug, info, warn ou error"},
	{"log-format", "LOG_FORMAT", "json", "json ou text"},
}

// Load interpreta os argumentos (sem o nome do programa) e o ambiente.
func Load(args []string, getenv func(string) string, stderr io.Writer) (*Config, error) {
	fs := flag.NewFlagSet("collector-cgibr", flag.ContinueOnError)
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
	c.SourceURL = *values["source-url"]
	c.UserAgent = *values["user-agent"]
	c.LogLevel = strings.ToLower(*values["log-level"])
	c.LogFormat = strings.ToLower(*values["log-format"])

	switch sha := *values["source-sha256-url"]; strings.ToLower(sha) {
	case "":
		c.SHA256URL = c.SourceURL + ".sha256"
	case "off", "none", "false":
		c.SHA256URL = ""
	default:
		c.SHA256URL = sha
	}
	for _, d := range []struct {
		flag string
		dst  *time.Duration
	}{{"sync-interval", &c.SyncInterval}, {"retry-interval", &c.RetryInterval}, {"run-timeout", &c.RunTimeout}} {
		if *d.dst, err = time.ParseDuration(*values[d.flag]); err != nil || *d.dst <= 0 {
			return nil, fmt.Errorf("--%s inválido: %q", d.flag, *values[d.flag])
		}
	}
	if c.MinASNs, err = strconv.Atoi(*values["min-asns"]); err != nil || c.MinASNs < 0 {
		return nil, fmt.Errorf("--min-asns inválido: %q", *values["min-asns"])
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
	if !strings.HasPrefix(c.SourceURL, "http://") && !strings.HasPrefix(c.SourceURL, "https://") {
		return nil, fmt.Errorf("--source-url precisa ser http(s): %q", c.SourceURL)
	}
	if !c.ShowVersion && c.PostgresURL == "" {
		return nil, fmt.Errorf("defina POSTGRES_URL (ou --postgres-url)")
	}
	return c, nil
}

// PrintHelp escreve o texto de ajuda.
func PrintHelp(w io.Writer) {
	fmt.Fprint(w, `collector-cgibr — importa o arquivo de ASNs e blocos do NIC.br (registro.br)
para as tabelas cgibr_* do PostgreSQL do BadBlock.

Verifica a fonte a cada SYNC_INTERVAL e só aplica quando o arquivo muda
(SHA-256 publicado, ETag/Last-Modified e hash do conteúdo). Não tem API HTTP.

Uso:
  collector-cgibr [opções]

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
