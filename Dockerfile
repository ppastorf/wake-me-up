FROM golang:1.25-alpine3.21 AS builder

RUN apk --update add ca-certificates git

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -a -ldflags '-extldflags "-s -w -static"' \
    -o /go/bin/wake-me-up \
    ./cmd/wake-me-up

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /go/bin/wake-me-up /usr/local/bin/wake-me-up

COPY sounds /etc/wake-me-up/sounds
COPY config /etc/wake-me-up/config
COPY static /etc/wake-me-up/static
COPY templates /etc/wake-me-up/templates

WORKDIR /etc/wake-me-up
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/wake-me-up"]
