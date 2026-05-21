FROM golang:1.25-alpine AS builder

WORKDIR /workspace

# Install dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags="-w -s" \
    -o sealos-notify .

# Final stage
FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /workspace/sealos-notify .

# Copy example config
COPY config.example.yaml /etc/sealos-notify/config.yaml

# Expose port
EXPOSE 8080

# Run
ENTRYPOINT ["/app/sealos-notify"]
CMD ["--config", "/etc/sealos-notify/config.yaml"]
