# syntax=docker/dockerfile:1
#
# postgres-migrate: a imagem do dbmate com o migrate.sh e as migrations de
# todas as pastas (central/ e uma por fonte) em /db. O container de migrate
# roda em qualquer servidor só com a imagem, sem montar a pasta do projeto.
# O .dockerignore deixa entrar só o migrate.sh e os <pasta>/*.sql.

FROM ghcr.io/amacneil/dbmate:2.36
ARG VERSION=dev
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.title="badblock-postgres-migrate" \
      org.opencontainers.image.description="Migrations do PostgreSQL do BadBlock (dbmate + migrate.sh)" \
      org.opencontainers.image.source="https://github.com/patrickbrandao/badblock" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.created="${BUILD_DATE}"
COPY . /db/
ENV MIGRATIONS_ROOT=/db
ENTRYPOINT ["/bin/sh", "/db/migrate.sh"]
CMD ["up"]
