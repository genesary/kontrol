FROM docker.io/golang:1.26.5-alpine AS build

WORKDIR /app

COPY . .

ENV GO111MODULE=on \
    CGO_ENABLED=0

RUN apk add --no-cache make git && \
  make build

FROM alpine:3.24.1 AS security_provider

RUN addgroup -S -g 1000 security-hub \
    && adduser -S -u 1000 -G security-hub security-hub

FROM scratch

# Add user / group 1000 to the image
COPY --from=security_provider /etc/passwd /etc/passwd
# Add default trusted certificates to the image
COPY --from=security_provider /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
# Add a writable /tmp directory to the image (required by scorecard)
COPY --from=security_provider --chown=1000:1000 --chmod=1777 /tmp /tmp

USER security-hub

COPY --from=build /app/bin/security-hub /usr/local/bin/security-hub

ENTRYPOINT [ "/usr/local/bin/security-hub" ]
