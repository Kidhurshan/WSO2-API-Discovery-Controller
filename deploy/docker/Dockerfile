# Multi-stage build for WSO2 API Discovery Controller
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=$(cat VERSION 2>/dev/null || echo dev)" \
    -o /build/adc ./cmd/adc/

# Runtime image
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S adc && adduser -S adc -G adc

COPY --from=builder /build/adc /usr/local/bin/adc

USER adc
EXPOSE 8090

ENTRYPOINT ["/usr/local/bin/adc"]
CMD ["--config", "/etc/adc/config.toml"]
