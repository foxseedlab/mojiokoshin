# Lint image
FROM golangci/golangci-lint:v2.10.1 AS golangci-lint

# Building image
FROM golang:1.26.0-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN apk add --no-cache opus-dev opusfile-dev pkgconfig gcc musl-dev git
RUN CGO_ENABLED=1 go build -tags opus -o /app/main cmd/backend/main.go

# Runtime image
# CGOと共有opusライブラリに依存しているため、distroless ではなくAlpineランタイムを採用する
FROM alpine:3.23.3 AS production

WORKDIR /app

RUN apk add --no-cache opus opusfile
COPY --from=builder /app/main /app/main

ENTRYPOINT ["/app/main"]


# Development image
FROM golang:1.26.0-alpine AS development

WORKDIR /app

RUN apk add --no-cache opus-dev opusfile-dev pkgconfig gcc musl-dev git
RUN go install github.com/air-verse/air@v1.64.5
COPY --from=golangci-lint /usr/bin/golangci-lint /usr/local/bin/golangci-lint

COPY go.mod go.sum ./
RUN go mod download

CMD ["air", "-c", ".air.toml"]
