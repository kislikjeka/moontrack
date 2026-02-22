FROM golang:1.24-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git && \
    git clone --depth 1 https://github.com/grafana/loki-mcp.git .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o loki-mcp-server ./cmd/server

FROM alpine:3.21
WORKDIR /app
COPY --from=builder /app/loki-mcp-server .
ENTRYPOINT ["./loki-mcp-server"]
