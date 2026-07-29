# Stage 1: Build the Go binary
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Step 1: Copy ONLY go.mod, download deps, generate go.sum
COPY go.mod ./
RUN go mod download

# Step 2: Now copy EVERYTHING else (source + web/)
COPY . .

# Step 3: Run tidy to ensure go.sum is complete (won't delete it now
#         because we already have all source files present)
RUN go mod tidy

# Step 4: Build static binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /silent-stress .

# Stage 2: Minimal runtime
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /silent-stress /silent-stress
EXPOSE 8080
ENTRYPOINT ["/silent-stress"]
