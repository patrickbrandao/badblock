// Package resolver é o cliente DNS do collector-rdns: pergunta o PTR de um
// IPv4 aos resolvedores recursivos de DNS_SERVERS, em rodízio, com prazo e
// novas tentativas, e transforma a resposta num estado (ptr, no_ptr,
// servfail, refused, timeout) e numa lista de nomes normalizada.
//
// As regras (o que cada resposta vira, quando tentar de novo, quando tirar um
// servidor do rodízio) estão em specs/fontes/rdns/fonte.md e collector.md.
package resolver

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/limiter"
)

// Status é o estado de um endereço depois da consulta. Os quatro primeiros
// são os códigos de rdns_ptr.status; NoPTR nunca vai para o banco (o IP sem
// PTR não tem linha).
type Status int16

const (
	PTR      Status = 1
	ServFail Status = 2
	Refused  Status = 3
	Timeout  Status = 4
	NoPTR    Status = 5
)

func (s Status) String() string {
	switch s {
	case PTR:
		return "ptr"
	case ServFail:
		return "servfail"
	case Refused:
		return "refused"
	case Timeout:
		return "timeout"
	case NoPTR:
		return "no_ptr"
	}
	return "unknown"
}

// Failed diz se o estado é uma falha (servfail, refused, timeout).
func (s Status) Failed() bool { return s == ServFail || s == Refused || s == Timeout }

// Result é o que a consulta de um endereço achou.
type Result struct {
	IP     netip.Addr
	Status Status
	Names  []string // só no PTR: sem repetição, em ordem de bytes
}

// Name é o primeiro nome (o que vai em rdns_ptr.name), ou "".
func (r Result) Name() string {
	if len(r.Names) == 0 {
		return ""
	}
	return r.Names[0]
}

const (
	// UDPSize é o tamanho de EDNS0 anunciado e o buffer do cliente.
	UDPSize = 1232
	// maxFailures falhas seguidas tiram um servidor do rodízio por downFor.
	maxFailures = 5
	downFor     = 30 * time.Second
)

// Options configura o Resolver.
type Options struct {
	Servers  []string         // host:porta
	Timeout  time.Duration    // prazo de cada tentativa (DNS_TIMEOUT)
	Retries  int              // novas tentativas (DNS_RETRIES)
	Inflight int              // consultas esperando resposta ao mesmo tempo (DNS_INFLIGHT)
	Limiter  *limiter.Limiter // toda consulta enviada passa por ele; nil = sem limite
	Log      *slog.Logger
	Now      func() time.Time // relógio da saúde dos servidores (testes)
}

type server struct {
	addr      string
	failures  int
	down      bool
	downUntil time.Time
}

// Resolver é seguro para uso concorrente.
type Resolver struct {
	timeout time.Duration
	retries int
	lim     *limiter.Limiter
	log     *slog.Logger
	now     func() time.Time
	sem     chan struct{}
	queries atomic.Int64

	mu      sync.Mutex
	servers []*server
	next    int
	allDown bool // já avisou que todos estão fora
}

// New cria o Resolver.
func New(o Options) *Resolver {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Inflight < 1 {
		o.Inflight = 1
	}
	r := &Resolver{timeout: o.Timeout, retries: o.Retries, lim: o.Limiter, log: o.Log, now: o.Now,
		sem: make(chan struct{}, o.Inflight)}
	for _, a := range o.Servers {
		r.servers = append(r.servers, &server{addr: a})
	}
	return r
}

// Queries é o total de consultas enviadas (com as novas tentativas e as
// repetições por TCP).
func (r *Resolver) Queries() int64 { return r.queries.Load() }

// Lookup pergunta o PTR de ip. first é a vez já reservada no limitador para a
// primeira consulta (a varredura reserva os endereços de um prefixo em
// ordem); nil reserva agora, na fila prio. O erro só vem quando o contexto
// acaba antes do fim: aí o resultado não vale.
func (r *Resolver) Lookup(ctx context.Context, ip netip.Addr, prio limiter.Priority, first *limiter.Ticket) (Result, error) {
	res := Result{IP: ip}
	qname, err := dns.ReverseAddr(ip.String())
	if err != nil {
		return res, err
	}
	last := Timeout // a última falha, gravada quando as tentativas acabam
	servfails := 0
	prev := -1
	for attempt := 0; attempt <= r.retries; attempt++ {
		i := r.pick(prev)
		prev = i
		ticket := first
		if attempt > 0 {
			ticket = nil
		}
		resp, err := r.query(ctx, ticket, prio, qname, i, "udp")
		if err == nil && resp.Truncated {
			// Truncada: a mesma pergunta por TCP, no mesmo servidor.
			resp, err = r.query(ctx, nil, prio, qname, i, "tcp")
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if err != nil || !sameQuestion(resp, qname) {
			// Sem resposta, erro de rede ou resposta a outra pergunta.
			r.failure(i)
			last = Timeout
			continue
		}
		switch resp.Rcode {
		case dns.RcodeSuccess, dns.RcodeNameError:
			r.success(i)
			if res.Names = ptrNames(resp); len(res.Names) > 0 {
				res.Status = PTR
			} else {
				res.Status = NoPTR
			}
			return res, nil
		case dns.RcodeServerFailure:
			// Resposta válida (o servidor está vivo), mas sem dado: mais um
			// servidor; o segundo SERVFAIL encerra.
			r.success(i)
			last = ServFail
			if servfails++; servfails >= 2 {
				res.Status = ServFail
				return res, nil
			}
		default:
			r.failure(i)
			last = Refused
		}
	}
	res.Status = last
	return res, nil
}

// query envia uma pergunta a um servidor, depois da vez no limitador e de uma
// vaga entre as consultas em voo.
func (r *Resolver) query(ctx context.Context, ticket *limiter.Ticket, prio limiter.Priority, qname string, i int, network string) (*dns.Msg, error) {
	if ticket == nil && r.lim != nil {
		ticket = r.lim.Reserve(prio)
	}
	if ticket != nil {
		if err := ticket.Wait(ctx); err != nil {
			return nil, err
		}
	}
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-r.sem }()
	r.queries.Add(1)

	m := new(dns.Msg)
	m.SetQuestion(qname, dns.TypePTR) // ID aleatório
	m.RecursionDesired = true
	m.SetEdns0(UDPSize, false) // DO desligado: o reverso não é validado
	c := &dns.Client{Net: network, Timeout: r.timeout, UDPSize: UDPSize}
	qctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	resp, _, err := c.ExchangeContext(qctx, m, r.addr(i))
	if err == nil && resp == nil {
		err = errors.New("resposta vazia")
	}
	return resp, err
}

func sameQuestion(m *dns.Msg, qname string) bool {
	if len(m.Question) != 1 {
		return false
	}
	q := m.Question[0]
	return q.Qtype == dns.TypePTR && q.Qclass == dns.ClassINET && strings.EqualFold(q.Name, qname)
}

// ptrNames devolve os PTR da seção de resposta, qualquer que seja o dono
// (no RFC 2317 o PTR é do nome para onde o CNAME aponta), normalizados, sem
// repetição e em ordem de bytes.
func ptrNames(m *dns.Msg) []string {
	seen := map[string]bool{}
	var out []string
	for _, rr := range m.Answer {
		p, ok := rr.(*dns.PTR)
		if !ok {
			continue
		}
		n := Normalize(p.Ptr)
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Normalize põe um nome na forma gravada: a forma de apresentação da
// miekg/dns (\DDD fora do ASCII imprimível, \. dentro de um rótulo), letras
// ASCII em minúsculas e sem o ponto final; o PTR que aponta para a raiz fica
// ".".
func Normalize(name string) string {
	b := []byte(name)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	if len(b) > 1 && b[len(b)-1] == '.' {
		b = b[:len(b)-1]
	}
	return string(b)
}

func (r *Resolver) addr(i int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.servers[i].addr
}

// pick escolhe o servidor da próxima tentativa: o seguinte no rodízio que
// esteja nele, diferente do da tentativa anterior quando há mais de um.
// Todos fora do rodízio: usa todos mesmo assim.
func (r *Resolver) pick(prev int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	healthy := 0
	for _, s := range r.servers {
		if s.down && !now.Before(s.downUntil) {
			r.log.Info("servidor DNS de volta ao rodízio", "server", s.addr, "failures", s.failures)
			s.down, s.failures = false, 0
		}
		if !s.down {
			healthy++
		}
	}
	n := len(r.servers)
	if healthy == 0 && !r.allDown {
		r.log.Warn("todos os servidores DNS fora do rodízio; usando todos")
	}
	r.allDown = healthy == 0
	for k := range n {
		i := (r.next + k) % n
		s := r.servers[i]
		if healthy > 0 && s.down {
			continue
		}
		if i == prev && (healthy > 1 || (healthy == 0 && n > 1)) {
			continue
		}
		r.next = (i + 1) % n
		return i
	}
	// Só o servidor da tentativa anterior está no rodízio.
	r.next = (prev + 1) % n
	return prev
}

func (r *Resolver) failure(i int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.servers[i]
	s.failures++
	if s.failures >= maxFailures && !s.down {
		s.down, s.downUntil = true, r.now().Add(downFor)
		r.log.Warn("servidor DNS fora do rodízio", "server", s.addr, "failures", s.failures)
	}
}

func (r *Resolver) success(i int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.servers[i].failures = 0
}
