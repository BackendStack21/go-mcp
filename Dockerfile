# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN CGO_ENABLED=0 go build -o /greeter ./examples/greet/

# Runtime stage — minimal scratch-like image
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /greeter /usr/local/bin/greeter
ENTRYPOINT ["/usr/local/bin/greeter"]
