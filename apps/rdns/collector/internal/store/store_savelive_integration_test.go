//go:build integration

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/resolver"
)

// Testes das tarefas 012 e 013 da auditoria de 07/11 (tasks/012 e tasks/013):
// o INSERT de rdns_ptr tolera a linha que nasceu noutra transação, e o
// SaveLive reconfere o prefixo na própria transação.

// waitBlocked espera até que alguma sessão esteja parada à espera de uma
// trava. É o sinal de que a transação concorrente chegou ao ponto em que a
// corrida acontece, sem depender de sleep.
func waitBlocked(t *testing.T, admin *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for count(t, admin, "SELECT count(*) FROM pg_locks WHERE NOT granted") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("nenhuma sessão ficou esperando trava")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ptrOf devolve o status e o primeiro nome da linha de rdns_ptr do IP
// (found = false se não houver linha).
func ptrOf(t *testing.T, admin *pgxpool.Pool, ip string) (status resolver.Status, name string, found bool) {
	t.Helper()
	var s int16
	var n *string
	err := admin.QueryRow(context.Background(), "SELECT status, name FROM rdns_ptr WHERE ip = $1::inet", ip).Scan(&s, &n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	if n != nil {
		name = *n
	}
	return resolver.Status(s), name, true
}

// TestInsertComConflito (tarefa 012): o Insert decidido sobre uma linha que
// já existe (porque a varredura e o tempo real leram "sem linha" ao mesmo
// tempo) vira UPDATE, sem erro e sem uma falha apagar um PTR.
func TestInsertComConflito(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()
	exec(t, admin, "INSERT INTO rdns_prefix (prefix, asn) VALUES ('200.160.2.0/24', 22548)")
	const ip = "200.160.2.10"
	insert := func(res resolver.Result) {
		t.Helper()
		var b pgx.Batch
		queueChange(&b, Insert, res)
		if err := st.pool.SendBatch(ctx, &b).Close(); err != nil {
			t.Fatalf("INSERT de %v: %v", res.Status, err)
		}
	}
	want := func(step string, status resolver.Status, name string) {
		t.Helper()
		got, gotName, found := ptrOf(t, admin, ip)
		if !found || got != status || gotName != name {
			t.Errorf("%s: linha = (%v, %q, found=%v), quero (%v, %q)", step, got, gotName, found, status, name)
		}
	}

	insert(ptrResult(ip, resolver.PTR, "a.example"))
	want("sem conflito", resolver.PTR, "a.example")

	insert(ptrResult(ip, resolver.Timeout))
	want("falha não apaga PTR", resolver.PTR, "a.example")

	insert(ptrResult(ip, resolver.PTR, "b.example"))
	want("PTR novo sobrescreve PTR", resolver.PTR, "b.example")

	exec(t, admin, "DELETE FROM rdns_ptr WHERE ip = $1::inet", ip)
	insert(ptrResult(ip, resolver.Timeout))
	want("falha sem PTR", resolver.Timeout, "")

	insert(ptrResult(ip, resolver.ServFail))
	want("falha sobrescreve falha", resolver.ServFail, "")

	insert(ptrResult(ip, resolver.PTR, "c.example"))
	want("PTR sobrescreve falha", resolver.PTR, "c.example")
}

// TestSaveLiveContraInsertNaoCommitado (tarefa 012): a corrida real. Uma
// transação (a "varredura") insere o IP e ainda não fez commit; o SaveLive leu
// "sem linha" e fica esperando no INSERT. Depois do commit ele não pode falhar
// com chave duplicada.
func TestSaveLiveContraInsertNaoCommitado(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()
	exec(t, admin, "INSERT INTO rdns_prefix (prefix, asn) VALUES ('200.160.2.0/24', 22548)")

	cases := []struct {
		name       string
		ip         string
		live       resolver.Status
		liveName   string
		wantStatus resolver.Status
		wantName   string
	}{
		{"PTR do tempo real vence", "200.160.2.21", resolver.PTR, "live.example", resolver.PTR, "live.example"},
		{"falha do tempo real não apaga o PTR da varredura", "200.160.2.22", resolver.Timeout, "", resolver.PTR, "scan.example"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scan, err := st.pool.Begin(ctx) // a "varredura", ainda sem commit
			if err != nil {
				t.Fatal(err)
			}
			defer scan.Rollback(ctx)
			var b pgx.Batch
			queueChange(&b, Insert, ptrResult(tc.ip, resolver.PTR, "scan.example"))
			if err := scan.SendBatch(ctx, &b).Close(); err != nil {
				t.Fatal(err)
			}

			var live resolver.Result
			if tc.live == resolver.PTR {
				live = ptrResult(tc.ip, resolver.PTR, tc.liveName)
			} else {
				live = ptrResult(tc.ip, tc.live)
			}
			errc := make(chan error, 1)
			go func() {
				_, err := st.SaveLive(ctx, live)
				errc <- err
			}()
			waitBlocked(t, admin) // o SaveLive chegou ao INSERT e está esperando
			if err := scan.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-errc:
				if err != nil {
					t.Errorf("SaveLive falhou depois do commit da outra transação: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("SaveLive não terminou")
			}
			got, name, found := ptrOf(t, admin, tc.ip)
			if !found || got != tc.wantStatus || name != tc.wantName {
				t.Errorf("linha = (%v, %q, found=%v), quero (%v, %q)", got, name, found, tc.wantStatus, tc.wantName)
			}
		})
	}
}

// TestSaveLiveReconferePrefixo (tarefa 013): SaveLive não grava IP sem
// prefixo ativo na regra.
func TestSaveLiveReconferePrefixo(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()
	live := func(ip string) error {
		_, err := st.SaveLive(ctx, ptrResult(ip, resolver.PTR, "live.example"))
		return err
	}

	// Sem prefixo cadastrado.
	if err := live("200.160.2.30"); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("sem prefixo: err = %v, quero ErrNotAllowed", err)
	}
	// Prefixo inativo.
	exec(t, admin, "INSERT INTO rdns_prefix (prefix, asn, active) VALUES ('200.160.2.0/24', 22548, false)")
	if err := live("200.160.2.31"); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("prefixo inativo: err = %v, quero ErrNotAllowed", err)
	}
	// Fora da regra (/22 a /24): um /16 estático.
	exec(t, admin, "INSERT INTO rdns_prefix (prefix, asn) VALUES ('10.9.0.0/16', 1)")
	if err := live("10.9.0.5"); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("fora da regra: err = %v, quero ErrNotAllowed", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM rdns_ptr"); n != 0 {
		t.Errorf("rdns_ptr tem %d linhas, quero 0", n)
	}

	// Prefixo ativo na regra: grava.
	exec(t, admin, "UPDATE rdns_prefix SET active = true WHERE prefix = '200.160.2.0/24'")
	if err := live("200.160.2.32"); err != nil {
		t.Errorf("prefixo ativo: err = %v", err)
	}
	if _, _, found := ptrOf(t, admin, "200.160.2.32"); !found {
		t.Error("o IP do prefixo ativo deveria ter sido gravado")
	}
}

// TestSaveLiveContraDeleteDoPrefixo (tarefa 013): o administrador apaga o
// prefixo (transação ainda aberta) enquanto o SaveLive grava um IP dele. O
// SaveLive espera o commit do DELETE e depois recusa, sem deixar linha órfã em
// rdns_ptr.
func TestSaveLiveContraDeleteDoPrefixo(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()
	exec(t, admin, "INSERT INTO rdns_prefix (prefix, asn) VALUES ('200.160.2.0/24', 22548)")
	const ip = "200.160.2.50"

	del, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer del.Rollback(ctx)
	if _, err := del.Exec(ctx, "DELETE FROM rdns_prefix WHERE prefix = '200.160.2.0/24'"); err != nil {
		t.Fatal(err)
	}

	errc := make(chan error, 1)
	go func() {
		_, err := st.SaveLive(ctx, ptrResult(ip, resolver.PTR, "live.example"))
		errc <- err
	}()
	waitBlocked(t, admin) // o SaveLive espera a trava do prefixo
	if err := del.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if !errors.Is(err, ErrNotAllowed) {
			t.Errorf("SaveLive = %v, quero ErrNotAllowed", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SaveLive não terminou")
	}
	if n := count(t, admin, "SELECT count(*) FROM rdns_ptr WHERE ip = $1::inet", ip); n != 0 {
		t.Errorf("sobrou %d linha(s) órfã(s) em rdns_ptr", n)
	}
}
