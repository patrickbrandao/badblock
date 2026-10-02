#!/bin/sh
# BadBlock — build local e push das imagens dos apps para o Docker Hub.
#
# É o caminho de publicação das imagens (não há CI publicando). Rode de uma
# máquina já autenticada (`docker login`) na conta tmsoftbrasil.
#
# A versão de cada app vem da tag git <app>/vX.Y.Z que aponta para o HEAD
# (o ./run-commit.sh cria as tags), então crie a tag antes:
#
#   git tag -a api-cgibr/v0.1.0 -m "api-cgibr v0.1.0"
#
# Cada imagem sai com DUAS tags: a versão e `latest`. A de versão existe porque
# `latest` é sobrescrita e não deixa cópia; o caminho de volta é
# API_CGIBR_TAG=<versão anterior> no .env do servidor (ou TAG no run-prod.sh).
#
# A `latest` só anda no fim, e de uma vez. O laço de build publica apenas a tag
# de versão; só depois que TODAS as imagens pedidas estão no Hub um segundo
# laço, curto, aponta `latest` para elas (`docker buildx imagetools create`:
# cópia do manifesto dentro do registry, sem rebuild nem download). Build que
# falha no meio deixa `latest` inteira na versão anterior, e o script diz em
# que estado o Hub ficou e o que rodar em seguida.
#
# Multi-plataforma: publicando de um Mac ARM sem `--platform`, o
# servidor amd64 receberia "no match for platform" no pull. Com pressa, uma
# arquitetura só: `PLATFORMS=linux/amd64 ./release-images.sh`. Exige um builder
# que conheça as duas arquiteturas (o `desktop-linux` do Docker Desktop
# conhece); faltando, o script recusa antes do primeiro build.
#
# O gate é UMA confirmação, mostrando o que vai subir: versões, plataformas e o
# commit — com aviso quando a árvore está suja, porque o build sai do disco, não
# do commit. Testes não rodam aqui: rode `make test test-int lint` antes. A procedência
# vai gravada na imagem (labels OCI `...image.version` e `...image.revision`) e
# no binário (`<app> --version`).
#
#   ./release-images.sh                 # os apps com tag no HEAD
#   ./release-images.sh api-cgibr       # só os nomes passados
#
# Rode sempre a partir da raiz do repositório.

set -eu

NAMESPACE="tmsoftbrasil"
APPS="collector-afrinic api-afrinic collector-apnic api-apnic collector-arin api-arin"
APPS="$APPS collector-asnames api-asnames collector-cgibr api-cgibr collector-iana api-iana"
APPS="$APPS collector-lacnic api-lacnic collector-ripencc api-ripencc"
APPS="$APPS collector-rootanchors api-rootanchors collector-roothints api-roothints"
APPS="$APPS collector-rootzone api-rootzone"
APPS="$APPS collector-anatel-pst api-anatel-pst"
APPS="$APPS website-www"

# Arquiteturas do manifesto publicado: servidor comum é amd64 e quem publica
# costuma estar em arm64.
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"

# Commit de origem, com `-sujo` quando há alteração não commitada. Vai para o
# label e para o binário: é a única forma de saber, depois, o que foi
# distribuído.
COMMIT="$(git rev-parse HEAD)"
[ -z "$(git status --porcelain)" ] || COMMIT="$COMMIT-sujo"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Versão de um app: a tag <app>/vX.Y.Z no HEAD (a maior, se houver mais de uma).
versao_de() {
    git tag --points-at HEAD --list "$1/v*" | sed "s|^$1/v||" | sort -V | tail -n 1
}

WANTED="$*"
for w in $WANTED; do
    case " $APPS " in
        *" $w "*) ;;
        *) echo "!! nenhum app com esse nome: $w (apps: $APPS)" >&2; exit 1 ;;
    esac
done

# A seleção é resolvida ANTES da confirmação: o que a confirmação mostra tem de
# ser exatamente o que vai subir.
SELECIONADAS=""
for app in $APPS; do
    if [ -n "$WANTED" ]; then
        case " $WANTED " in
            *" $app "*) ;;
            *) continue ;;
        esac
    fi

    versao="$(versao_de "$app")"
    if [ -z "$versao" ]; then
        # Sem nomes, publica só quem tem tag; com nomes, a falta de tag é erro.
        [ -z "$WANTED" ] && continue
        echo "!! $app não tem tag $app/vX.Y.Z no HEAD. Crie antes:" >&2
        echo "   git tag -a $app/vX.Y.Z -m \"$app vX.Y.Z\"" >&2
        exit 1
    fi
    # SemVer: X.Y.Z, com sufixo opcional (-rc.1).
    if ! printf '%s' "$versao" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'; then
        echo "!! a tag $app/v$versao não segue SemVer (app/vX.Y.Z)" >&2
        exit 1
    fi

    SELECIONADAS="$SELECIONADAS $app:$versao"
done

if [ -z "$SELECIONADAS" ]; then
    echo "!! nenhum app com tag <app>/vX.Y.Z no HEAD. Crie a tag antes, por exemplo:" >&2
    echo "   git tag -a api-cgibr/v0.1.0 -m \"api-cgibr v0.1.0\"" >&2
    exit 1
fi

# buildx é obrigatório: `docker build` + `docker push` não montam manifesto
# com mais de uma arquitetura.
if ! docker buildx inspect >/dev/null 2>&1; then
    echo "!! docker buildx indisponível. Crie um builder e repita:" >&2
    echo "   docker buildx create --use --name badblock --driver docker-container" >&2
    exit 1
fi

# Plataforma que o builder não conhece só falha no meio do build, com mensagem
# obscura: melhor recusar antes do primeiro.
DISPONIVEIS="$(docker buildx inspect | sed -n 's/^ *Platforms: *//p' | tr -d ' ' | tr '\n' ',')"
if [ -n "$DISPONIVEIS" ]; then
    for p in $(echo "$PLATFORMS" | tr ',' ' '); do
        case ",$DISPONIVEIS" in
            *",$p,"*) ;;
            *)
                echo "!! o builder atual não constrói $p (conhece: $DISPONIVEIS)" >&2
                echo "   docker buildx create --use --name badblock --driver docker-container" >&2
                exit 1
                ;;
        esac
    done
fi

echo "== commit:      $COMMIT"
echo "== plataformas: $PLATFORMS"
for entry in $SELECIONADAS; do
    echo "== imagem:      $NAMESPACE/badblock-${entry%%:*}  (tags :${entry#*:} e :latest)"
done
case "$COMMIT" in
    *-sujo)
        echo "!! árvore suja: o build sai do disco, então alteração não commitada"
        echo "!! e não revisada VAI para a imagem publicada."
        ;;
esac
printf '== publicar no Docker Hub (%s)? [s/N] ' "$NAMESPACE"
read -r RESPOSTA || RESPOSTA=""
case "$RESPOSTA" in
    s|S|sim|SIM) ;;
    *) echo "== cancelado."; exit 1 ;;
esac

# Falha no meio não pode ser silenciosa: o `set -e` derruba o script no primeiro
# comando que falha, e quem publica precisa saber em que estado o Hub ficou e o
# que rodar em seguida. O trap só entra aqui, depois da confirmação, para não
# falar nas saídas de antes (nome errado, builder ausente, "cancelado").
FASE="build"
PUBLICADAS=""
PROMOVIDAS=""

ao_sair() {
    status=$?
    case "$FASE" in
        build)
            echo "!! interrompido no build (status $status). Nenhuma :latest foi movida: o Hub" >&2
            echo "!! continua inteiro na versão anterior. Já no Hub só com a tag de versão:${PUBLICADAS:- nenhuma}." >&2
            echo "!! Corrija e repita o MESMO comando — o que já foi construído sai do cache do builder." >&2
            ;;
        promocao)
            echo "!! interrompido na promoção (status $status). Todas as tags de versão estão no Hub," >&2
            echo "!! mas a :latest só foi movida em:${PROMOVIDAS:- nenhuma}. Falta mover:" >&2
            for entry in $SELECIONADAS; do
                app="${entry%%:*}"
                image="$NAMESPACE/badblock-$app"
                case " $PROMOVIDAS " in
                    *" $app "*) ;;
                    *) echo "!!   docker buildx imagetools create -t $image:latest $image:${entry#*:}" >&2 ;;
                esac
            done
            ;;
    esac
}
trap ao_sair EXIT
# Ctrl-C e `kill` viram `exit` para passarem pelo trap acima: no dash, morte por
# sinal não dispara o trap de EXIT.
trap 'exit 130' INT
trap 'exit 143' TERM

for entry in $SELECIONADAS; do
    app="${entry%%:*}"
    versao="${entry#*:}"
    image="$NAMESPACE/badblock-$app"

    # collector-<fonte> e api-<fonte> moram em apps/<fonte>/<collector|api>;
    # numa fonte de dois níveis o "-" do nome vira "/" no caminho
    # (collector-anatel-pst → apps/anatel/pst/collector); website-<site>, em
    # websites/<site>.
    case "$app" in
        website-*) contexto="websites/${app#website-}" ;;
        *) contexto="apps/$(echo "${app#*-}" | tr - /)/${app%%-*}" ;;
    esac
    echo "== build: $image:$versao ($PLATFORMS, contexto: $contexto)"
    # Build e push num passo só: a imagem multi-plataforma não cabe no store
    # local do Docker, então não há o que empurrar depois com `docker push`.
    # Só a tag de versão sai daqui — a `latest` é do laço seguinte.
    docker buildx build \
        --platform "$PLATFORMS" \
        --build-arg "VERSION=$versao" \
        --build-arg "COMMIT=$COMMIT" \
        --build-arg "BUILD_DATE=$BUILD_DATE" \
        --label "org.opencontainers.image.version=$versao" \
        --label "org.opencontainers.image.revision=$COMMIT" \
        -t "$image:$versao" \
        --push "$contexto"
    PUBLICADAS="$PUBLICADAS $app:$versao"
done

# Só agora `latest` anda. Com uma origem só, que já é um manifest list,
# `imagetools create` faz cópia fiel dentro do registry — mesmo digest, logo as
# mesmas plataformas e os mesmos labels da tag de versão —, sem rebuild e sem
# download.
FASE="promocao"
for entry in $SELECIONADAS; do
    app="${entry%%:*}"
    image="$NAMESPACE/badblock-$app"
    echo "== latest: $image:latest -> :${entry#*:}"
    docker buildx imagetools create -t "$image:latest" "$image:${entry#*:}"
    PROMOVIDAS="$PROMOVIDAS $app"
done
FASE="fim"

echo "== Publicado em $PLATFORMS:$SELECIONADAS (e latest)."
echo "== A tag git ainda é só local? Envie com ./run-commit.sh push (ou git push origin <app>/vX.Y.Z)."
