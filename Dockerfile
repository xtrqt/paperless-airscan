FROM golang:1.24-alpine AS builder

ARG CACHEBUST=1

RUN apk add --no-cache git build-base

WORKDIR /app

COPY go.mod go.sum ./
COPY third_party/ ./third_party/
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /paperless-airscan ./cmd/paperless-airscan

FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata font-liberation

RUN adduser -D -g '' appuser

WORKDIR /app

COPY --from=builder /paperless-airscan /app/paperless-airscan

RUN mkdir -p /data && chown appuser:appuser /data

USER appuser

VOLUME ["/data"]

EXPOSE 8080

CMD ["/app/paperless-airscan"]
