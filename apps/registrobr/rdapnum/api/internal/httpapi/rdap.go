package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"
	"strings"
)

// O corpo RDAP cru sai como o registro.br mandou, menos as linhas de
// notices[*].description que mostram quem consultou ("<hora> - <ip>"): é o
// IP de saída do coletor, que nunca sai na API
// (specs/fontes/registrobr/rdapnum/api-rotas.md#resposta-crua).

// egressLine é a linha do aviso que o coletor lê (fonte.md).
var egressLine = regexp.MustCompile(`^\S+ - \S+$`)

// hidesEgress diz se uma linha de description sai do corpo: a linha
// "<hora> - <ip>" e, por segurança, qualquer linha com um endereço IP solto
// (o formato pode mudar; o IP não pode vazar).
func hidesEgress(line string) bool {
	if egressLine.MatchString(strings.TrimSpace(line)) {
		return true
	}
	for f := range strings.FieldsSeq(line) {
		f = strings.Trim(f, "()[],;\"'")
		if _, err := netip.ParseAddr(f); err == nil {
			return true
		}
	}
	return false
}

// member é um par chave/valor de um objeto JSON, na ordem do texto.
type member struct {
	key string
	val json.RawMessage
}

// readObject lê um objeto JSON preservando a ordem das chaves.
func readObject(b []byte) ([]member, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("não é um objeto JSON")
	}
	var out []member
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("chave inválida")
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, member{key, v})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return out, nil
}

// writeObject escreve os membros na ordem dada.
func writeObject(ms []member) []byte {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, _ := json.Marshal(m.key)
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(m.val)
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

// filterNotice tira as linhas do IP de saída de um item de notices.
func filterNotice(raw json.RawMessage) (json.RawMessage, error) {
	ms, err := readObject(raw)
	if err != nil {
		return raw, nil //nolint:nilerr // item que não é objeto: não tem description para filtrar
	}
	for i, m := range ms {
		if m.key != "description" {
			continue
		}
		var lines []json.RawMessage
		if json.Unmarshal(m.val, &lines) != nil {
			continue
		}
		kept := []json.RawMessage{}
		for _, l := range lines {
			var s string
			if json.Unmarshal(l, &s) == nil && hidesEgress(s) {
				continue
			}
			kept = append(kept, l)
		}
		b, err := json.Marshal(kept)
		if err != nil {
			return nil, err
		}
		ms[i].val = b
	}
	return writeObject(ms), nil
}

// publicRDAP devolve o corpo RDAP compacto, na ordem das chaves em que veio,
// sem as linhas do IP de saída em notices, com uma quebra de linha no fim.
func publicRDAP(body string) ([]byte, error) {
	ms, err := readObject([]byte(body))
	if err != nil {
		return nil, err
	}
	for i, m := range ms {
		if m.key != "notices" {
			continue
		}
		var items []json.RawMessage
		if err := json.Unmarshal(m.val, &items); err != nil {
			// notices fora do formato: some inteiro (não dá para garantir
			// que não leva o IP).
			ms[i].val = json.RawMessage("[]")
			continue
		}
		for j, it := range items {
			if items[j], err = filterNotice(it); err != nil {
				return nil, err
			}
		}
		b, err := json.Marshal(items)
		if err != nil {
			return nil, err
		}
		ms[i].val = b
	}
	var out bytes.Buffer
	if err := json.Compact(&out, writeObject(ms)); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}
