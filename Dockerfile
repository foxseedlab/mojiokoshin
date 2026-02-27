# Building image
FROM golang:1.26.0-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o main cmd/backend/main.go


# Development image
FROM golang:1.26.0-alpine AS development

WORKDIR /app

RUN go install github.com/air-verse/air@v1.64.5

COPY go.mod go.sum ./
RUN go mod download

CMD ["air", "-c", ".air.toml"]
