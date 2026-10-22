package limiter

import (
	"context"
	"testing"
	"time"
)

func TestInterval(t *testing.T) {
	if d := IntervalFor(32); d != 1875*time.Millisecond {
		t.Errorf("32/min = %v", d)
	}
	if d := IntervalFor(8); d != 7500*time.Millisecond {
		t.Errorf("8/min = %v", d)
	}
	if d := IntervalFor(0); d != time.Minute {
		t.Errorf("0/min = %v", d)
	}
}

func TestWaitSpacing(t *testing.T) {
	l := New(6000) // uma a cada 10 ms
	ctx := context.Background()
	start := time.Now()
	for range 6 {
		if err := l.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// A primeira sai na hora; as outras cinco, 10 ms depois da anterior.
	if el := time.Since(start); el < 50*time.Millisecond || el > 500*time.Millisecond {
		t.Errorf("6 consultas em %v", el)
	}
	if l.Rate() != 6000 {
		t.Errorf("Rate = %d", l.Rate())
	}
}

func TestNoBurstAfterIdle(t *testing.T) {
	l := New(600) // 100 ms
	ctx := context.Background()
	_ = l.Wait(ctx)
	time.Sleep(350 * time.Millisecond) // parado: não acumula vezes
	start := time.Now()
	_ = l.Wait(ctx)
	_ = l.Wait(ctx)
	if el := time.Since(start); el < 90*time.Millisecond {
		t.Errorf("rajada depois de parado: %v", el)
	}
}

func TestSetRateDuringWait(t *testing.T) {
	l := New(1) // um minuto
	ctx := context.Background()
	_ = l.Wait(ctx)
	done := make(chan time.Duration)
	go func() {
		start := time.Now()
		_ = l.Wait(ctx)
		done <- time.Since(start)
	}()
	time.Sleep(50 * time.Millisecond)
	l.SetRate(60000) // 1 ms: a espera em curso acaba
	select {
	case d := <-done:
		if d > 2*time.Second {
			t.Errorf("espera = %v", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a troca de taxa não valeu para a espera em curso")
	}
}

func TestWaitCanceled(t *testing.T) {
	l := New(1)
	_ = l.Wait(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := l.Wait(ctx); err == nil {
		t.Error("Wait sem erro com o contexto vencido")
	}
}
