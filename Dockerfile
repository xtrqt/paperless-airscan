FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git build-base

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /paperless-airscan ./cmd/paperless-airscan

FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

RUN adduser -D -g '' appuser

WORKDIR /app

COPY --from=builder /paperless-airscan /app/paperless-airscan

RUN mkdir -p /data && chown appuser:appuser /data

USER appuser

VOLUME ["/data"]

EXPOSE 8080

CMD ["/app/paperless-airscan"]
