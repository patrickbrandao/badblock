#!/bin/sh
# Verificação das specs do BadBlock (make specs-check). Falha se:
#   - um link relativo de um .md das specs, do AGENTS.md, do CLAUDE.md, do
#     README.md, dos .md dos apps, dos sites e de database/ ou dos sub-agentes
#     aponta para um arquivo que não existe, ou para uma âncora (#seção) que
#     não existe no .md de destino (links dentro de blocos de código não
#     contam; o nome da âncora segue a regra do GitHub: minúsculas, sem
#     pontuação, espaços viram hífens, títulos repetidos ganham -1, -2...);
#   - uma fonte em apps/ (apps/<fonte>/ ou apps/<site>/<conjunto>/) não tem
#     specs/fontes/<fonte>/ com os cinco arquivos, database/postgres/<fonte>/,
#     os dois apps e os dois sub-agentes, com o nome na forma de cada lugar
#     (ou sobra pasta de spec sem app);
#   - sobra uma pasta specs/ dentro de um app, ou algum arquivo (docs,
#     sub-agentes, código, manifestos, Makefiles, SQL) ainda cita o caminho
#     antigo apps/<fonte>/<tipo>/specs/;
#   - um arquivo de specs/ fora das pastas de fonte não está no mapa do
#     specs/README.md, ou um arquivo de uma pasta de fonte não está no
#     README.md dela.
#
# Rode da raiz do repositório (o make já faz isso). Só sh, find e awk.
set -eu

cd "$(dirname "$0")/.."

erros=0
falha() {
	echo "specs-check: $*" >&2
	erros=$((erros + 1))
}

# links <arquivo.md>: imprime os destinos dos links markdown, fora de blocos
# de código e de trechos entre crases.
links() {
	awk '
		/^[[:space:]]*```/ { fence = !fence; next }
		fence { next }
		{
			line = $0
			gsub(/`[^`]*`/, "", line)
			while (match(line, /\]\([^)]*\)/)) {
				print substr(line, RSTART + 2, RLENGTH - 3)
				line = substr(line, RSTART + RLENGTH)
			}
		}
	' "$1"
}

# destinos <arquivo.md>: os links relativos do arquivo, resolvidos a partir da
# pasta dele, sem âncora e sem título.
destinos() {
	dir=$(dirname "$1")
	links "$1" | while IFS= read -r t; do
		t=${t%% *}
		t=${t%%#*}
		case "$t" in
		"" | http://* | https://* | mailto:*) continue ;;
		esac
		echo "$dir/$t"
	done
}

# normaliza <caminho>: tira "./" e resolve "pasta/../" (sem tocar o disco).
normaliza() {
	echo "$1" | awk -F/ '{
		n = 0
		for (i = 1; i <= NF; i++) {
			if ($i == "" || $i == ".") continue
			if ($i == ".." && n > 0 && p[n] != "..") { n--; continue }
			p[++n] = $i
		}
		s = ""
		for (i = 1; i <= n; i++) s = s (i > 1 ? "/" : "") p[i]
		print s
	}'
}

# 1. Links quebrados.
arquivos=$(find specs apps database websites .claude/agents -name '*.md' \
	-not -path '*/bin/*' -not -path '*/testdata/*' -not -path '*/node_modules/*' -not -path '*/vendor/*' \
	-not -path '*/dist/*' 2>/dev/null | sort)
for f in AGENTS.md CLAUDE.md README.md $arquivos; do
	[ -f "$f" ] || continue
	destinos "$f" | while IFS= read -r d; do
		[ -e "$d" ] || echo "$f: link quebrado: ${d#"$(dirname "$f")/"}"
	done
done >"${TMPDIR:-/tmp}/specs-check.$$"
if [ -s "${TMPDIR:-/tmp}/specs-check.$$" ]; then
	while IFS= read -r l; do falha "$l"; done <"${TMPDIR:-/tmp}/specs-check.$$"
fi
rm -f "${TMPDIR:-/tmp}/specs-check.$$"

# 1b. Âncoras: o #nome de cada link para um .md tem de existir no destino.
# Passo 1 guarda as âncoras de cada arquivo; passo 2 confere os links.
# shellcheck disable=SC2086
awk '
	function lower(s,   i, c, k, o) {
		o = ""
		for (i = 1; i <= length(s); i++) {
			c = substr(s, i, 1)
			k = index(UP, c)
			o = o (k ? substr(LO, k, 1) : c)
		}
		return o
	}
	function slug(h) {
		gsub(/`/, "", h)
		sub(/^[ \t]+/, "", h)
		sub(/[ \t#]+$/, "", h)
		h = lower(h)
		gsub(/Á/, "á", h); gsub(/À/, "à", h); gsub(/Â/, "â", h); gsub(/Ã/, "ã", h)
		gsub(/É/, "é", h); gsub(/Ê/, "ê", h); gsub(/Í/, "í", h); gsub(/Ó/, "ó", h)
		gsub(/Ô/, "ô", h); gsub(/Õ/, "õ", h); gsub(/Ú/, "ú", h); gsub(/Ç/, "ç", h)
		gsub(PUNCT, "", h)
		gsub(/—|–|…|→|←|↔|×|·|“|”|‘|’|«|»|≈|≥|≤|±|°|•/, "", h)
		gsub(/ /, "-", h)
		return h
	}
	function norm(p,   n, i, a, k, s) {
		n = split(p, a, "/")
		k = 0
		for (i = 1; i <= n; i++) {
			if (a[i] == "" || a[i] == ".") continue
			if (a[i] == ".." && k > 0 && q[k] != "..") { k--; continue }
			q[++k] = a[i]
		}
		s = ""
		for (i = 1; i <= k; i++) s = s (i > 1 ? "/" : "") q[i]
		return s
	}
	BEGIN {
		UP = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		LO = "abcdefghijklmnopqrstuvwxyz"
		PUNCT = "[!-,./:-@[-^`{-~]"
	}
	FNR == 1 { fence = 0; if (pass == 1) known[FILENAME] = 1 }
	/^[[:space:]]*```/ { fence = !fence; next }
	fence { next }
	pass == 1 && /^#+[ \t]/ {
		h = $0
		sub(/^#+[ \t]+/, "", h)
		s = slug(h)
		n = seen[FILENAME, s]++
		have[FILENAME, (n == 0 ? s : s "-" n)] = 1
		next
	}
	pass == 2 {
		line = $0
		gsub(/`[^`]*`/, "", line)
		while (match(line, /\]\([^)]*\)/)) {
			t = substr(line, RSTART + 2, RLENGTH - 3)
			line = substr(line, RSTART + RLENGTH)
			sub(/ .*/, "", t)
			if (t ~ /^(https?|mailto):/ || index(t, "#") == 0) continue
			a = substr(t, index(t, "#") + 1)
			p = substr(t, 1, index(t, "#") - 1)
			if (p == "") {
				f = FILENAME
			} else {
				d = FILENAME
				if (sub(/\/[^\/]*$/, "", d) == 0) d = "."
				f = norm(d "/" p)
			}
			if (f !~ /\.md$/ || !(f in known)) continue
			if (!((f, a) in have)) print FILENAME ":" FNR ": âncora #" a " não existe em " f
		}
	}
' pass=1 AGENTS.md CLAUDE.md README.md $arquivos pass=2 AGENTS.md CLAUDE.md README.md $arquivos >"${TMPDIR:-/tmp}/specs-check-ancoras.$$"
while IFS= read -r l; do falha "$l"; done <"${TMPDIR:-/tmp}/specs-check-ancoras.$$"
rm -f "${TMPDIR:-/tmp}/specs-check-ancoras.$$"

# 2. Uma pasta de spec, de migrations e dois sub-agentes por fonte. Fonte de
# dois níveis (apps/<site>/<conjunto>/, decisão #21): o caminho usa "/", os
# nomes de app "-" e a pasta de migrations "_".
fontes=""
for app in apps/*/; do
	[ -d "$app" ] || continue
	f=$(basename "$app")
	if [ -d "apps/$f/collector" ] || [ -d "apps/$f/api" ]; then
		fontes="$fontes $f"
		continue
	fi
	for sub in "apps/$f"/*/; do
		[ -d "$sub" ] || continue
		fontes="$fontes $f/$(basename "$sub")"
	done
done
for fonte in $fontes; do
	nome=$(echo "$fonte" | tr / -)
	sql=$(echo "$fonte" | tr / _)
	for tipo in collector api; do
		[ -d "apps/$fonte/$tipo" ] || falha "apps/$fonte/$tipo não existe"
		[ -f ".claude/agents/$tipo-$nome.md" ] || falha "falta o sub-agente .claude/agents/$tipo-$nome.md"
		[ ! -d "apps/$fonte/$tipo/specs" ] || falha "sobrou apps/$fonte/$tipo/specs/ (as specs moram em specs/fontes/$fonte/)"
	done
	[ -d "database/postgres/$sql" ] || falha "falta database/postgres/$sql/"
	for arq in README fonte dados collector api; do
		[ -f "specs/fontes/$fonte/$arq.md" ] || falha "falta specs/fontes/$fonte/$arq.md"
	done
done
antigos=$(grep -rlIE 'apps/[a-z0-9<>]+/(collector|api)/specs' \
	AGENTS.md CLAUDE.md README.md specs apps database .claude/agents \
	--exclude-dir=bin --exclude-dir=testdata --exclude-dir=vendor --exclude=check.sh 2>/dev/null || true)
for f in $antigos; do
	falha "$f cita o caminho antigo apps/<fonte>/<tipo>/specs/ (use specs/fontes/<fonte>/)"
done
for d in specs/fontes/*/; do
	fonte=$(basename "$d")
	[ "$fonte" = rir ] && continue
	if [ -f "specs/fontes/$fonte/fonte.md" ]; then
		[ -d "apps/$fonte" ] || falha "specs/fontes/$fonte/ sem apps/$fonte/"
		continue
	fi
	# Pasta de site: só o README.md e uma pasta por conjunto.
	[ -f "specs/fontes/$fonte/README.md" ] || falha "falta specs/fontes/$fonte/README.md"
	for sub in "specs/fontes/$fonte"/*/; do
		[ -d "$sub" ] || continue
		c=$(basename "$sub")
		[ -d "apps/$fonte/$c" ] || falha "specs/fontes/$fonte/$c/ sem apps/$fonte/$c/"
	done
done
for arq in README formato dados collector api; do
	[ -f "specs/fontes/rir/$arq.md" ] || falha "falta specs/fontes/rir/$arq.md"
done

# 2b. Um sub-agente por site.
for site in websites/*/; do
	[ -d "$site" ] || continue
	s=$(basename "$site")
	[ -f ".claude/agents/website-$s.md" ] || falha "falta o sub-agente .claude/agents/website-$s.md"
done

# 3. Mapa: todo arquivo de specs/ tem de estar num índice.
no_indice() { # <índice> <arquivo>
	destinos "$1" | while IFS= read -r d; do normaliza "$d"; done | grep -qxF "$(normaliza "$2")"
}
for f in $(find specs -name '*.md' | sort); do
	case "$f" in
	specs/README.md) continue ;;
	specs/fontes/rir/*) indice=specs/README.md ;;
	specs/fontes/*/README.md) indice=specs/README.md ;;
	specs/fontes/README.md) indice=specs/README.md ;;
	specs/fontes/*/*) indice="$(dirname "$f")/README.md" ;;
	*) indice=specs/README.md ;;
	esac
	no_indice "$indice" "$f" || falha "$f não está no índice $indice"
done

if [ "$erros" -gt 0 ]; then
	echo "specs-check: $erros problema(s)" >&2
	exit 1
fi
echo "specs-check: ok"
