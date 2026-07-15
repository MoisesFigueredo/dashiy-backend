FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o /api ./cmd/api
RUN CGO_ENABLED=0 go build -o /migrate ./cmd/migrate

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /api /api
COPY --from=builder /migrate /migrate

EXPOSE 8080

CMD ["/bin/sh", "-c", "/migrate -direction up && exec /api"]
