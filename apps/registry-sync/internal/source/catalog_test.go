package source

import (
	"os"
	"strings"
	"testing"
)

func TestCatalogIsConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Catalog() {
		if seen[s.ID] {
			t.Errorf("id repetido %q", s.ID)
		}
		seen[s.ID] = true
		if len(s.URLs) == 0 {
			t.Errorf("%s sem URL", s.ID)
		}
		for _, u := range s.URLs {
			if !strings.HasPrefix(u, "https://") {
				t.Errorf("%s: URL sem https: %s", s.ID, u)
			}
		}
		if (s.Kind == KindDelegated) != (s.RIR != "") {
			t.Errorf("%s: RIR só vale (e é obrigatório) para delegated", s.ID)
		}
		if s.MinRecords <= 0 {
			t.Errorf("%s sem mínimo de registros", s.ID)
		}
		// Toda fonte do catálogo tem fixture para os testes e o e2e.
		if _, err := os.Stat("../../testdata/sources/" + s.ID); err != nil {
			t.Errorf("%s sem fixture em testdata/sources: %v", s.ID, err)
		}
	}
}

func TestSelect(t *testing.T) {
	got, err := Select([]string{"nicbr", "rir-lacnic"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "rir-lacnic" || got[1].ID != "nicbr" {
		t.Errorf("Select deveria manter a ordem do catálogo: %v", got)
	}
	if _, err := Select([]string{"rir-lacnik"}); err == nil {
		t.Error("id desconhecido deveria gerar erro")
	}
	all, _ := Select(nil)
	if len(all) != len(Catalog()) {
		t.Error("lista vazia deveria devolver o catálogo inteiro")
	}
}
