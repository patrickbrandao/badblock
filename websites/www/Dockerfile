# syntax=docker/dockerfile:1
#
# website-www: site estático (Vite + React) servido por lighttpd.
# O dist/ é compilado aqui, nunca copiado da máquina de quem compila.

FROM --platform=$BUILDPLATFORM node:24-alpine AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
RUN npm run build

FROM alpine:3.20

RUN apk add --no-cache lighttpd

COPY lighttpd.docker.conf /etc/lighttpd/lighttpd.conf

COPY --from=build /src/dist/ /var/www/localhost/htdocs/
COPY landing-2/ /var/www/localhost/htdocs/landing-2/

RUN mkdir -p /var/log/lighttpd

EXPOSE 8080

CMD ["lighttpd", "-D", "-f", "/etc/lighttpd/lighttpd.conf"]
