# Stage 1: build the Go binary
FROM golang:1.25-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /spendlint ./cmd/spendlint

# Stage 2: runtime with Node (for mcp-remote) + the binary
FROM node:22-bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
# Pre-install mcp-remote so the first request is not slow
RUN npm install -g mcp-remote
COPY --from=builder /spendlint /usr/local/bin/spendlint
# Bake in the cached mcp-remote OAuth tokens so Cloud Run can call GitLab MCP
# without a browser consent flow.
COPY mcp-auth /root/.mcp-auth

ENV PORT=8080
ENV SPENDLINT_DB=/data/spendlint.db
VOLUME ["/data"]

EXPOSE 8080
ENTRYPOINT ["spendlint", "serve"]
