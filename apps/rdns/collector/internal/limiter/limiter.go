// Package limiter controla a taxa de consultas DNS de uma instância do
// collector-rdns: libera uma consulta a cada 1/rate_limit segundos, sem
// rajada, com duas filas — a do tempo real é sempre servida antes da da
// varredura.
//
// Quem quer enviar uma consulta reserva a vez (Reserve) e espera a liberação
// (Ticket.Wait). A varredura reserva todos os endereços de um prefixo de uma
// vez, em ordem, e por isso Waiting diz quantas consultas da varredura ainda
// esperam: é por ele que o laço da fila decide pegar o próximo prefixo.
package limiter

import (
	"context"
	"sync"
	"time"
)

// Priority é a fila de uma reserva.
type Priority int

const (
	// Live é a fila do tempo real, servida primeiro.
	Live Priority = iota
	// Scan é a fila da varredura.
	Scan
)

// Ticket é a vez de uma consulta.
type Ticket struct {
	l        *Limiter
	prio     Priority
	ch       chan struct{} // fechado na liberação
	canceled bool          // desistiu antes da vez (protegido por l.mu)
	released bool          // protegido por l.mu
}

// Wait espera a vez da consulta. Com o contexto encerrado antes da vez, a
// reserva é cancelada (não gasta a vez de ninguém) e o erro do contexto volta.
func (t *Ticket) Wait(ctx context.Context) error {
	select {
	case <-t.ch:
		return nil
	default:
	}
	select {
	case <-t.ch:
		return nil
	case <-ctx.Done():
		t.l.cancel(t)
		// A liberação pode ter chegado junto com o cancelamento.
		select {
		case <-t.ch:
			return nil
		default:
			return ctx.Err()
		}
	}
}

// Limiter é o limitador de uma instância.
type Limiter struct {
	mu       sync.Mutex
	interval time.Duration
	queues   [2][]*Ticket
	pending  [2]int
	last     time.Time // última liberação
	wake     chan struct{}
	now      func() time.Time
}

// New cria um limitador de rate consultas por segundo (mínimo 1). Ele só
// libera consultas depois de Run.
func New(rate int) *Limiter {
	l := &Limiter{wake: make(chan struct{}, 1), now: time.Now}
	l.interval = intervalFor(rate)
	return l
}

func intervalFor(rate int) time.Duration {
	if rate < 1 {
		rate = 1
	}
	return time.Second / time.Duration(rate)
}

// SetRate troca a taxa sem reiniciar nada; vale a partir da próxima liberação.
func (l *Limiter) SetRate(rate int) {
	l.mu.Lock()
	l.interval = intervalFor(rate)
	l.mu.Unlock()
	l.signal()
}

// Rate devolve a taxa em vigor, em consultas por segundo.
func (l *Limiter) Rate() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return int(time.Second / l.interval)
}

// Reserve põe uma consulta na fila p e devolve a vez dela.
func (l *Limiter) Reserve(p Priority) *Ticket {
	t := &Ticket{l: l, prio: p, ch: make(chan struct{})}
	l.mu.Lock()
	l.queues[p] = append(l.queues[p], t)
	l.pending[p]++
	l.mu.Unlock()
	l.signal()
	return t
}

// Wait reserva e espera a vez de uma consulta na fila p.
func (l *Limiter) Wait(ctx context.Context, p Priority) error {
	return l.Reserve(p).Wait(ctx)
}

// Waiting diz quantas consultas da fila p ainda esperam a vez.
func (l *Limiter) Waiting(p Priority) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.pending[p]
}

func (l *Limiter) cancel(t *Ticket) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !t.released && !t.canceled {
		t.canceled = true
		l.pending[t.prio]--
	}
}

func (l *Limiter) signal() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// next tira da frente da fila a próxima reserva viva (tempo real primeiro),
// sem liberá-la. Devolve nil com as duas filas vazias.
func (l *Limiter) next() *Ticket {
	for p := range l.queues {
		for len(l.queues[p]) > 0 {
			t := l.queues[p][0]
			if t.canceled {
				l.queues[p][0] = nil
				l.queues[p] = l.queues[p][1:]
				continue
			}
			return t
		}
	}
	return nil
}

// Run libera as consultas até o contexto acabar: uma a cada intervalo, a da
// fila do tempo real antes da da varredura. Parado por um tempo, libera a
// próxima na hora (sem acumular vezes: não há rajada).
func (l *Limiter) Run(ctx context.Context) {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		l.mu.Lock()
		t := l.next()
		var wait time.Duration
		if t != nil {
			if due := l.last.Add(l.interval); !l.last.IsZero() {
				wait = due.Sub(l.now())
			}
			if wait <= 0 {
				// A vez chegou: libera a da frente da fila mais prioritária.
				l.queues[t.prio][0] = nil
				l.queues[t.prio] = l.queues[t.prio][1:]
				l.pending[t.prio]--
				t.released = true
				close(t.ch)
				l.last = l.now()
				l.mu.Unlock()
				continue
			}
		}
		l.mu.Unlock()

		if t == nil {
			select {
			case <-ctx.Done():
				return
			case <-l.wake:
			}
			continue
		}
		// Espera a vez; uma reserva nova (talvez do tempo real) ou uma troca
		// de taxa refazem a conta.
		timer.Reset(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-l.wake:
			timer.Stop()
		}
	}
}
