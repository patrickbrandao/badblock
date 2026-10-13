package limiter

import (
	"context"
	"sync"
	"testing"
	"time"
)

// releaseTimes espera n reservas na fila p e devolve o momento de cada
// liberação, na ordem das reservas.
func releaseTimes(t *testing.T, l *Limiter, p Priority, n int) []time.Time {
	t.Helper()
	tickets := make([]*Ticket, n)
	for i := range tickets {
		tickets[i] = l.Reserve(p)
	}
	out := make([]time.Time, n)
	for i, tk := range tickets {
		if err := tk.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
		out[i] = time.Now()
	}
	return out
}

func TestInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l := New(100) // uma consulta a cada 10 ms
	go l.Run(ctx)

	start := time.Now()
	times := releaseTimes(t, l, Scan, 21)
	elapsed := times[len(times)-1].Sub(start)
	// 21 consultas = a primeira na hora e mais 20 intervalos de 10 ms.
	if elapsed < 195*time.Millisecond || elapsed > 600*time.Millisecond {
		t.Errorf("21 consultas a 100/s levaram %v, quero ~200ms", elapsed)
	}
	for i := 1; i < len(times); i++ {
		if d := times[i].Sub(times[i-1]); d < 9*time.Millisecond {
			t.Errorf("liberação %d veio %v depois da anterior (rajada)", i, d)
		}
	}
	if l.Waiting(Scan) != 0 {
		t.Errorf("Waiting = %d", l.Waiting(Scan))
	}
}

func TestLiveBeforeScan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l := New(50) // 20 ms

	var mu sync.Mutex
	var order []string
	var wg sync.WaitGroup
	wait := func(tk *Ticket, name string) {
		wg.Go(func() {
			if err := tk.Wait(ctx); err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
		})
	}
	// Cinco da varredura na fila antes de um do tempo real; o limitador só
	// começa a liberar depois.
	for _, n := range []string{"s1", "s2", "s3", "s4", "s5"} {
		wait(l.Reserve(Scan), n)
	}
	wait(l.Reserve(Live), "live")
	if l.Waiting(Scan) != 5 || l.Waiting(Live) != 1 {
		t.Fatalf("Waiting = %d/%d", l.Waiting(Scan), l.Waiting(Live))
	}
	go l.Run(ctx)
	wg.Wait()
	if order[0] != "live" {
		t.Errorf("ordem = %v; o tempo real deveria sair primeiro", order)
	}
	want := []string{"live", "s1", "s2", "s3", "s4", "s5"}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("ordem = %v, quero %v", order, want)
			break
		}
	}
}

func TestSetRate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l := New(10) // 100 ms
	go l.Run(ctx)

	start := time.Now()
	releaseTimes(t, l, Scan, 3) // 0, 100, 200 ms
	slow := time.Since(start)
	if slow < 190*time.Millisecond {
		t.Errorf("3 consultas a 10/s levaram %v", slow)
	}

	l.SetRate(1000) // 1 ms, sem reiniciar
	if l.Rate() != 1000 {
		t.Errorf("Rate = %d", l.Rate())
	}
	start = time.Now()
	releaseTimes(t, l, Scan, 20)
	if fast := time.Since(start); fast > 150*time.Millisecond {
		t.Errorf("20 consultas a 1000/s levaram %v", fast)
	}
}

func TestCanceledTicketDoesNotSpendTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l := New(20) // 50 ms

	gone, giveUp := context.WithCancel(context.Background())
	a := l.Reserve(Scan)
	b := l.Reserve(Scan)
	giveUp()
	if err := a.Wait(gone); err == nil {
		t.Fatal("Wait com o contexto encerrado deveria falhar")
	}
	if l.Waiting(Scan) != 1 {
		t.Errorf("Waiting = %d, quero 1", l.Waiting(Scan))
	}
	go l.Run(ctx)
	start := time.Now()
	if err := b.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 30*time.Millisecond {
		t.Errorf("a reserva cancelada gastou a vez: b saiu em %v", d)
	}
}
