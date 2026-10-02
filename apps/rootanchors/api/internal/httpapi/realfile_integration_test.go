//go:build integration

package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/store"
	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/testdb"
)

// fixtureFile é o root-anchors.xml real inteiro (2026-09-30), fixture do
// collector-rootanchors.
var fixtureFile = filepath.Join("..", "..", "..", "collector", "testdata", "root-anchors.xml")

// TestRealFile carrega o root-anchors.xml real com o binário do collector
// (../collector, compilado aqui, com o arquivo servido por um
// httptest.Server) num PG18 descartável e confere as rotas sobre esses dados:
// cada key tag, o DS e o DNSKEY em texto (recalculando o digest SHA-256 e o
// key tag a partir do DNSKEY), a meta, tamanhos e tempos. Sem
// ROOTANCHORS_REAL_FILE, usa a fixture do collector (o arquivo real inteiro de
// 2026-09-30); com ela, o arquivo baixado com curl:
//
//	make test-real FILE=/tmp/root-anchors.xml
func TestRealFile(t *testing.T) {
	path := os.Getenv("ROOTANCHORS_REAL_FILE")
	if path == "" {
		path = fixtureFile
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	pgURL, admin := testdb.New(t)

	// Compila o collector irmão e carrega o arquivo com --once (sem a
	// conferência do checksums-sha256.txt, que não é servido aqui).
	bin := filepath.Join(t.TempDir(), store.CollectorApp)
	build := exec.Command("go", "build", "-o", bin, "./cmd/"+store.CollectorApp)
	build.Dir = filepath.Join("..", "..", "..", "collector")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build do collector: %v\n%s", err, out)
	}
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write(raw)
	}))
	defer files.Close()
	run := exec.Command(bin, "--once")
	run.Env = append(os.Environ(), "POSTGRES_URL="+pgURL, "SOURCE_URL="+files.URL+"/root-anchors.xml",
		"SOURCE_SHA256_URL=off", "LOG_FORMAT=text")
	start := time.Now()
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("collector --once: %v\n%s", err, out)
	}
	t.Logf("arquivo %s (%d bytes); carga pelo collector: %v", path, len(raw), time.Since(start).Round(time.Millisecond))

	st, err := store.Open(ctx, pgURL, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	w := dataset.NewWatcher(st, time.Minute, log)
	if err := w.Refresh(ctx); err != nil || !w.Current().Ready() {
		t.Fatalf("dataset: %+v, %v", w.Current(), err)
	}
	rip, _ := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	h := New(st, w, cache.Noop{}, rip, log, Config{BasePath: base, Version: "real"}).Handler()

	get := func(path string, want int) *httptest.ResponseRecorder {
		t.Helper()
		start := time.Now()
		rec := do(h, "GET", base+path)
		if rec.Code != want {
			t.Errorf("%s: %d, quero %d: %s", path, rec.Code, want, rec.Body)
		}
		t.Logf("%-22s %3d %6d bytes %v", path, rec.Code, rec.Body.Len(), time.Since(start).Round(time.Microsecond))
		return rec
	}

	var rows int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM rootanchors_key`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	meta := decode[MetaResponse](t, get("/meta", 200))
	if meta.Dataset == nil || meta.Dataset.Keys != rows || meta.Dataset.TrustAnchor.Zone != "." ||
		meta.Dataset.TrustAnchor.ID == "" || meta.Collector == nil || meta.Collector.LastSyncAt == nil {
		t.Errorf("meta = %+v, %d chaves na tabela", meta.Dataset, rows)
	}
	sum := sha256.Sum256(raw)
	if meta.Dataset != nil && meta.Dataset.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("sha256 da meta = %s", meta.Dataset.SHA256)
	}

	all := decode[KeysResponse](t, get("/keys", 200))
	if all.Count != rows || len(all.Keys) != rows || all.Dataset.Version != w.Current().Version {
		t.Fatalf("/keys: %d chaves, tabela %d", all.Count, rows)
	}
	for i, k := range all.Keys {
		if i > 0 && k.ValidFrom < all.Keys[i-1].ValidFrom {
			t.Errorf("fora de ordem: %s antes de %s", all.Keys[i-1].KeyID, k.KeyID)
		}
		want := ". IN DS " + strconv.Itoa(k.KeyTag) + " " + strconv.Itoa(k.Algorithm) + " " + strconv.Itoa(k.DigestType) + " " + k.Digest
		if k.DS != want {
			t.Errorf("%s: ds = %q, quero %q", k.KeyID, k.DS, want)
		}
		if (k.PublicKey == nil) != (k.DNSKEY == nil) || (k.PublicKey == nil) != (k.Flags == nil) {
			t.Errorf("%s: public_key, flags e dnskey vêm juntos: %+v", k.KeyID, k)
		}
		if k.DNSKEY != nil {
			checkDNSKEY(t, k)
		}
		byTag := decode[KeyTagResponse](t, get("/key/"+strconv.Itoa(k.KeyTag), 200))
		found := false
		for _, x := range byTag.Keys {
			found = found || reflect.DeepEqual(x, k)
		}
		if !found || byTag.KeyTag != k.KeyTag || !reflect.DeepEqual(byTag.TrustAnchor, all.TrustAnchor) {
			t.Errorf("/key/%d não traz %s igual a /keys: %+v", k.KeyTag, k.KeyID, byTag)
		}
	}
	get("/v1/keys", 200)
	get("/key/1", 404)
	get("/key/65536", 400)
	get("/status", 200)

	// Tempos sem cache (média de 200 pedidos).
	for _, p := range []string{"/keys", "/key/20326"} {
		start := time.Now()
		for range 200 {
			if rec := do(h, "GET", base+p); rec.Code != 200 {
				t.Fatalf("%s: %d", p, rec.Code)
			}
		}
		t.Logf("%s: %v em média (sem cache)", p, (time.Since(start) / 200).Round(time.Microsecond))
	}
}

// checkDNSKEY confere o texto do DNSKEY de uma chave com o DS dela: recalcula
// o key tag (RFC 4034, apêndice B) e o digest SHA-256 (RFC 4034, 5.1.4) a
// partir de "<zona> IN DNSKEY <flags> 3 <algorithm> <public_key>".
func checkDNSKEY(t *testing.T, k Key) {
	t.Helper()
	f := strings.Fields(*k.DNSKEY)
	if len(f) != 7 || f[0] != "." || f[1] != "IN" || f[2] != "DNSKEY" || f[3] != strconv.Itoa(*k.Flags) || f[4] != "3" ||
		f[5] != strconv.Itoa(k.Algorithm) || f[6] != *k.PublicKey {
		t.Errorf("%s: dnskey = %q", k.KeyID, *k.DNSKEY)
		return
	}
	pub, err := base64.StdEncoding.DecodeString(f[6])
	if err != nil {
		t.Errorf("%s: public_key: %v", k.KeyID, err)
		return
	}
	rdata := binary.BigEndian.AppendUint16(nil, uint16(*k.Flags))
	rdata = append(rdata, 3, byte(k.Algorithm))
	rdata = append(rdata, pub...)
	var ac uint32
	for i, b := range rdata {
		if i&1 == 0 {
			ac += uint32(b) << 8
		} else {
			ac += uint32(b)
		}
	}
	ac += ac >> 16
	if tag := int(ac & 0xffff); tag != k.KeyTag {
		t.Errorf("%s: key tag calculado %d, publicado %d", k.KeyID, tag, k.KeyTag)
	}
	if k.DigestType == 2 {
		sum := sha256.Sum256(append([]byte{0}, rdata...))
		if got := strings.ToUpper(hex.EncodeToString(sum[:])); got != k.Digest {
			t.Errorf("%s: digest calculado %s, publicado %s", k.KeyID, got, k.Digest)
		}
	}
}
