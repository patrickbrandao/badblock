// Package limiter controla a taxa de consultas RDAP de uma instância do
// collector-registrobr-rdapnum: libera uma consulta a cada 60 s / rate_limit,
// sem rajada (decisão #41: a taxa é por minuto e por coletor). Toda consulta
// passa por aqui antes de sair, qualquer que seja o resultado dela.
package limiter

import (
	"context"
	"sync"
	"time"
)

// Limiter é o limitador de uma instância.
type Limiter struct {
	turn sync.Mutex // uma espera por vez: as vezes saem em ordem

	mu       sync.Mutex
	interval time.Duration
	last     time.Time // última liberação
	changed  chan struct{}
	now      func() time.Time
}

// New cria um limitador de rate consultas por minuto (mínimo 1).
func New(rate int) *Limiter {
	return &Limiter{interval: IntervalFor(rate), changed: make(chan struct{}, 1), now: time.Now}
}

// IntervalFor é o intervalo entre duas consultas a rate por minuto.
func IntervalFor(rate int) time.Duration {
	if rate < 1 {
		rate = 1
	}
	return time.Minute / time.Duration(rate)
}

// SetRate troca a taxa sem reiniciar nada; vale para a espera em curso.
func (l *Limiter) SetRate(rate int) {
	l.mu.Lock()
	l.interval = IntervalFor(rate)
	l.mu.Unlock()
	select {
	case l.changed <- struct{}{}:
	default:
	}
}

// Rate devolve a taxa em vigor, em consultas por minuto.
func (l *Limiter) Rate() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return int(time.Minute / l.interval)
}

// Wait espera a vez de uma consulta: a primeira sai na hora; as seguintes,
// um intervalo depois da anterior. Parado por um tempo, a próxima sai na hora
// (sem acumular vezes: não há rajada). Com o contexto encerrado antes da vez,
// devolve o erro do contexto e a vez não é gasta.
func (l *Limiter) Wait(ctx context.Context) error {
	l.turn.Lock()
	defer l.turn.Unlock()
	for {
		l.mu.Lock()
		now := l.now()
		due := l.last.Add(l.interval)
		if l.last.IsZero() || !now.Before(due) {
			l.last = now
			l.mu.Unlock()
			return nil
		}
		wait := due.Sub(now)
		l.mu.Unlock()
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		case <-l.changed:
			t.Stop()
		}
	}
}
