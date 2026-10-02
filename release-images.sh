#!/bin/sh
# BadBlock — build local e push das imagens dos apps para o Docker Hub.
#
# É o caminho de publicação das imagens (não há CI publicando). Rode de uma
# máquina já autenticada (`docker login`) na conta tmsoftbrasil.
#
# Publica a partir dos fontes do disco. A versão é a do projeto, a última tag
# git vX.Y.Z (ex.: v0.0.1 → imagens :0.0.1), igual para todos os apps; troque
# com `VERSION=0.0.2 ./release-images.sh`. Nada na tag nem nos labels depende
# do commit ou de haver alteração não commitada.
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
# O gate é UMA confirmação, mostrando o que vai subir: versão, plataformas e
# imagens. Testes não rodam aqui: rode `make test test-int lint` antes. A versão
# vai gravada na imagem (label OCI `...image.version`) e no binário
# (`<app> --version`).
#
#   ./release-images.sh                 # todos os apps
#   ./release-images.sh api-cgibr       # só os nomes passados
#
# Rode sempre a partir da raiz do repositório.

set -eu

NAMESPACE="tmsoftbrasil"
APPS="collector-afrinic api-afrinic collector-apnic api-apnic collector-arin api-arin"
APPS="$APPS collector-ripe-asnames api-ripe-asnames collector-cgibr api-cgibr collector-iana api-iana"
APPS="$APPS collector-lacnic api-lacnic collector-ripencc api-ripencc"
APPS="$APPS collector-rootanchors api-rootanchors collector-roothints api-roothints"
APPS="$APPS collector-rootzone api-rootzone"
APPS="$APPS collector-anatel-pst api-anatel-pst"
APPS="$APPS website-www"

# Arquiteturas do manifesto publicado: servidor comum é amd64 e quem publica
# costuma estar em arm64.
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"

BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Versão do projeto: VERSION do ambiente ou a última tag vX.Y.Z do git.
if [ -z "${VERSION:-}" ]; then
    VERSION="$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null | sed 's|^v||')"
fi
if [ -z "$VERSION" ]; then
    echo "!! sem versão: crie a tag do projeto (git tag -a v0.0.1 -m \"BadBlock 0.0.1\") ou passe VERSION=X.Y.Z" >&2
    exit 1
fi
# SemVer: X.Y.Z, com sufixo opcional (-rc.1).
if ! printf '%s' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$'; then
    echo "!! a versão $VERSION não segue SemVer (X.Y.Z)" >&2
    exit 1
fi

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

    SELECIONADAS="$SELECIONADAS $app:$VERSION"
done

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

echo "== versão:      $VERSION"
echo "== plataformas: $PLATFORMS"
for entry in $SELECIONADAS; do
    echo "== imagem:      $NAMESPACE/badblock-${entry%%:*}  (tags :$VERSION e :latest)"
done
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
        --build-arg "BUILD_DATE=$BUILD_DATE" \
        --label "org.opencontainers.image.version=$versao" \
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
