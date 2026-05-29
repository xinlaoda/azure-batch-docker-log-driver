# Build stage
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /azure-batch-docker-log-driver .

# Plugin rootfs stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates
RUN mkdir -p /run/docker/plugins

COPY --from=builder /azure-batch-docker-log-driver /azure-batch-docker-log-driver
