# Stage 1: Build the Go binary
FROM golang:1.23-alpine AS builder

# Install git for module downloads, ca-certificates for HTTPS
RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go.mod and go.sum first for dependency caching
COPY go.mod go.sum ./
RUN go mod download

# Copy entire source (including web/ folder for embed)
COPY . .

# Build a static binary with the web assets embedded
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /silent-stress .

# Stage 2: Minimal runtime image
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /silent-stress /silent-stress

# Expose the port Railway will map
EXPOSE 8080

# Run the binary
ENTRYPOINT ["/silent-stress"]
