// Package rdap é o cliente HTTP do RDAP do registro.br: monta a URL de um
// alvo, faz o GET (pelo proxy de PROXY_URL, se houver, com prazo, corpo até
// 1 MiB e até 3 redirecionamentos no mesmo host) e classifica a resposta em
// completa, truncada, 404 ou erro (fonte.md#resposta-truncada,
// collector.md#coleta-de-um-alvo). Lê também, do aviso de toda resposta, a
// hora do servidor e o IP de saída que ele viu.
package rdap

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Result é a classificação de uma consulta (os valores de
// registrobr_rdapnum_target.last_result e _response.result).
type Result int16

const (
	Complete  Result = 1
	Truncated Result = 2
	NotFound  Result = 3
	Error     Result = 4
)

func (r Result) String() string {
	switch r {
	case Complete:
		return "complete"
	case Truncated:
		return "truncated"
	case NotFound:
		return "not_found"
	case Error:
		return "error"
	}
	return "unknown"
}

// Conclusive diz se a consulta conclui a coleta do alvo (completa ou 404).
func (r Result) Conclusive() bool { return r == Complete || r == NotFound }

// TruncatedRemark é o type do remarks que marca a resposta truncada.
const TruncatedRemark = "object truncated due to server policy"

// MaxBody é o maior corpo aceito.
const MaxBody = 1 << 20

// Kinds dos alvos e o objectClassName esperado de cada um.
const (
	KindASN    = "asn"
	KindPrefix = "prefix"
)

// ExpectedClass é o objectClassName esperado para um kind.
func ExpectedClass(kind string) string {
	if kind == KindASN {
		return "autnum"
	}
	return "ip network"
}

// Options configura o Client.
type Options struct {
	BaseURL   string        // RDAP_URL, sem a barra final
	Proxy     *url.URL      // PROXY_URL (nil = saída direta)
	Timeout   time.Duration // HTTP_TIMEOUT
	UserAgent string
	TLS       *tls.Config // só testes (certificado do servidor falso)
}

// Client faz as consultas.
type Client struct {
	base  string
	ua    string
	proxy bool
	hc    *http.Client
	to    time.Duration
}

// New cria o Client.
func New(opt Options) *Client {
	tr := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        4,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig:     opt.TLS,
	}
	if opt.Proxy != nil {
		tr.Proxy = http.ProxyURL(opt.Proxy)
	}
	hc := &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 {
				return errors.New("mais de 3 redirecionamentos")
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("redirecionamento para outro host: %s", req.URL.Host)
			}
			return nil
		},
	}
	return &Client{base: opt.BaseURL, ua: opt.UserAgent, proxy: opt.Proxy != nil, hc: hc, to: opt.Timeout}
}

// URL monta a URL da consulta de um alvo: /autnum/<asn> ou /ip/<bloco>.
func URL(base, kind, target string) string {
	if kind == KindASN {
		return base + "/autnum/" + target
	}
	return base + "/ip/" + target
}

// Response é o resultado de uma consulta.
type Response struct {
	Result     Result
	URL        string
	HTTPStatus int    // 0 = sem resposta HTTP (rede, prazo, proxy)
	Body       []byte // corpo como veio (nil em erro e no 404 sem JSON)
	Bytes      int    // tamanho do corpo recebido
	EgressIP   netip.Addr
	ServerTime time.Time
	Err        error // motivo, quando Result = Error
	ProxyErr   bool  // o erro veio do proxy
	Elapsed    time.Duration
}

// Fetch consulta um alvo. Nunca devolve erro à parte: o erro vira Result =
// Error com Err. O cancelamento de ctx também vira Error (quem chama confere
// ctx.Err()).
func (c *Client) Fetch(ctx context.Context, kind, target string) Response {
	start := time.Now()
	r := Response{URL: URL(c.base, kind, target)}
	defer func() { r.Elapsed = time.Since(start) }()
	ctx, cancel := context.WithTimeout(ctx, c.to)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL, nil)
	if err != nil {
		r.Result, r.Err = Error, err
		return r
	}
	req.Header.Set("Accept", "application/rdap+json")
	req.Header.Set("User-Agent", c.ua)
	resp, err := c.hc.Do(req)
	if err != nil {
		r.Result, r.Err = Error, err
		r.ProxyErr = c.proxy && IsProxyError(err)
		return r
	}
	defer resp.Body.Close()
	r.HTTPStatus = resp.StatusCode
	if resp.StatusCode == http.StatusProxyAuthRequired {
		r.Result, r.Err, r.ProxyErr = Error, errors.New("HTTP 407 do proxy"), c.proxy
		return r
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	r.Bytes = len(body)
	if err != nil {
		r.Result, r.Err = Error, fmt.Errorf("lendo o corpo: %w", err)
		return r
	}
	if len(body) > MaxBody {
		r.Result, r.Err = Error, errors.New("corpo maior que 1 MiB")
		return r
	}
	r.Result, r.Err = Classify(resp.StatusCode, body, ExpectedClass(kind))
	switch r.Result {
	case Complete, Truncated:
		r.Body = body
	case NotFound:
		if len(bytes.TrimSpace(body)) > 0 && json.Valid(body) {
			r.Body = body
		}
	}
	if r.Body != nil {
		r.ServerTime, r.EgressIP, _ = ReadNotice(r.Body)
	}
	return r
}

// IsProxyError diz se um erro do http.Client veio do proxy (conexão com o
// proxy, CONNECT recusado, SOCKS).
func IsProxyError(err error) bool {
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "proxyconnect" {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "proxyconnect") || strings.Contains(s, "socks connect") ||
		strings.Contains(s, "Proxy Authentication Required")
}

// object é o que a classificação lê de uma resposta.
type object struct {
	ObjectClassName string `json:"objectClassName"`
	Remarks         []struct {
		Type string `json:"type"`
	} `json:"remarks"`
}

// Classify classifica uma resposta HTTP já lida (fonte.md#resposta-truncada).
func Classify(status int, body []byte, expected string) (Result, error) {
	switch {
	case status == http.StatusNotFound:
		return NotFound, nil
	case status != http.StatusOK:
		return Error, fmt.Errorf("HTTP %d", status)
	}
	var o object
	if err := json.Unmarshal(body, &o); err != nil {
		return Error, fmt.Errorf("JSON inválido: %w", err)
	}
	if o.ObjectClassName != expected {
		return Error, fmt.Errorf("objectClassName inesperado: %q", o.ObjectClassName)
	}
	for _, rm := range o.Remarks {
		if strings.EqualFold(rm.Type, TruncatedRemark) {
			return Truncated, nil
		}
	}
	return Complete, nil
}

var noticeLine = regexp.MustCompile(`^(\S+) - (\S+)$`)

// ReadNotice lê do aviso de nível superior a hora do servidor e o IP de
// quem consultou: a segunda linha de description de um notices
// (fonte.md#aviso-com-o-ip-de-quem-consultou). ok = false sem a linha.
func ReadNotice(body []byte) (time.Time, netip.Addr, bool) {
	var o struct {
		Notices []struct {
			Description []string `json:"description"`
		} `json:"notices"`
	}
	if err := json.Unmarshal(body, &o); err != nil {
		return time.Time{}, netip.Addr{}, false
	}
	for _, n := range o.Notices {
		if len(n.Description) < 2 {
			continue
		}
		m := noticeLine.FindStringSubmatch(strings.TrimSpace(n.Description[1]))
		if m == nil {
			continue
		}
		t, err1 := time.Parse(time.RFC3339, m[1])
		ip, err2 := netip.ParseAddr(m[2])
		if err1 != nil || err2 != nil {
			continue
		}
		return t, ip.Unmap(), true
	}
	return time.Time{}, netip.Addr{}, false
}
