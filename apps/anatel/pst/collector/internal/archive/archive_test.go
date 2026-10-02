package archive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"
)

type entry struct {
	name     string
	data     string
	modified time.Time // zero = sem data
	ntfs     bool      // com o timestamp estendido (o archive/zip grava o 0x5455)
}

// makeZip monta um ZIP na memória. Sem ntfs, só o campo MS-DOS é gravado
// (como no Python ou no zip do Windows sem timestamp estendido).
func makeZip(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if !e.modified.IsZero() {
			if e.ntfs {
				h.Modified = e.modified
			} else {
				// Relógio MS-DOS sem timestamp estendido.
				h.ModifiedDate, h.ModifiedTime = dosFields(e.modified) //nolint:staticcheck // teste do campo MS-DOS
			}
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func dosFields(t time.Time) (uint16, uint16) {
	d := uint16(t.Day() + int(t.Month())<<5 + (t.Year()-1980)<<9)
	tm := uint16(t.Second()/2 + t.Minute()<<5 + t.Hour()<<11)
	return d, tm
}

func TestExtractSample(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/pst-sample.zip")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../testdata/pst-sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Extract(raw, MaxCSVBytes)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(want)
	if c.Name != "prestadoras_servicos_telecomunicacoes.csv" || !bytes.Equal(c.Data, want) ||
		c.SHA256 != hex.EncodeToString(sum[:]) || len(c.Ignored) != 0 {
		t.Errorf("csv = %s, %d bytes, %s, ignoradas %v", c.Name, len(c.Data), c.SHA256, c.Ignored)
	}
	// 2026-09-30 06:15:08 em Brasília = 09:15:08 UTC.
	if !c.ModifiedAt.Equal(time.Date(2026, 9, 30, 9, 15, 8, 0, time.UTC)) {
		t.Errorf("ModifiedAt = %v", c.ModifiedAt)
	}
}

func TestExtractDOSTimeIsBrasilia(t *testing.T) {
	local := time.Date(2026, 9, 29, 6, 15, 10, 0, time.UTC) // relógio de parede
	c, err := Extract(makeZip(t, entry{name: "a.csv", data: "x", modified: local}), 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 9, 29, 9, 15, 10, 0, time.UTC); !c.ModifiedAt.Equal(want) {
		t.Errorf("ModifiedAt = %v, quero %v", c.ModifiedAt, want)
	}
}

func TestExtractIgnoresExtendedTimestamp(t *testing.T) {
	// Com timestamp estendido, o archive/zip grava o instante real e o campo
	// MS-DOS com o relógio de parede do fuso de Modified; vale o MS-DOS.
	modified := time.Date(2026, 9, 30, 6, 15, 8, 0, Brasilia)
	c, err := Extract(makeZip(t, entry{name: "a.csv", data: "x", modified: modified, ntfs: true}), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !c.ModifiedAt.Equal(modified) {
		t.Errorf("ModifiedAt = %v, quero %v", c.ModifiedAt, modified)
	}
}

func TestExtractWithoutDate(t *testing.T) {
	c, err := Extract(makeZip(t, entry{name: "a.csv", data: "x"}), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !c.ModifiedAt.IsZero() {
		t.Errorf("sem data MS-DOS, ModifiedAt deveria ser zero: %v", c.ModifiedAt)
	}
}

func TestExtractIgnoresOtherEntries(t *testing.T) {
	z := makeZip(t, entry{name: "LEIAME.txt", data: "oi"}, entry{name: "dados/PST.CSV", data: "x;y"}, entry{name: "dados/", data: ""})
	c, err := Extract(z, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "dados/PST.CSV" || string(c.Data) != "x;y" || strings.Join(c.Ignored, ",") != "LEIAME.txt,dados/" {
		t.Errorf("csv = %+v", c)
	}
}

func TestExtractRejects(t *testing.T) {
	cases := []struct {
		name string
		zip  []byte
		max  int64
		msg  string
	}{
		{"não é ZIP", []byte("<html>erro</html>"), 0, "ZIP inválido: zip: not a valid zip file"},
		{"sem CSV", makeZip(t, entry{name: "a.txt", data: "x"}), 0, "ZIP sem CSV"},
		{"dois CSVs", makeZip(t, entry{name: "a.csv", data: "x"}, entry{name: "b.CSV", data: "y"}), 0, "ZIP com 2 arquivos CSV: a.csv, b.CSV"},
		{"grande", makeZip(t, entry{name: "a.csv", data: strings.Repeat("x", 100)}), 10, "CSV a.csv maior que 10 bytes descomprimido"},
	}
	for _, tc := range cases {
		_, err := Extract(tc.zip, tc.max)
		if err == nil || err.Error() != tc.msg {
			t.Errorf("%s: err = %v, quero %q", tc.name, err, tc.msg)
		}
	}
}

func TestExtractRejectsCorruptEntry(t *testing.T) {
	z := makeZip(t, entry{name: "a.csv", data: strings.Repeat("abc;def\r\n", 1000)})
	// Estraga o meio dos dados comprimidos (o cabeçalho local tem 30 bytes + o nome).
	z[40] ^= 0xff
	z[41] ^= 0xff
	if _, err := Extract(z, 0); err == nil || !strings.HasPrefix(err.Error(), "lendo a.csv do ZIP: ") {
		t.Fatalf("err = %v", err)
	}
}
