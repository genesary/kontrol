FROM docker.io/golang:1.26.5-alpine AS build

WORKDIR /app

COPY . .

ENV GO111MODULE=on \
    CGO_ENABLED=0

RUN apk add --no-cache make git && \
  make build

FROM alpine:3.24.1 AS security_provider

RUN addgroup -S security-hub \
    && adduser -S security-hub -G security-hub

FROM scratch

COPY --from=security_provider /etc/passwd /etc/passwd

USER security-hub

COPY --from=build /app/bin/security-hub /usr/local/bin/security-hub

ENTRYPOINT [ "/usr/local/bin/security-hub" ]
