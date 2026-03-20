FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o x_sticker_bot ./cmd/x_sticker_bot

FROM alpine:3.21

ENV TZ=UTC

RUN apk add --no-cache \
        ffmpeg \
        imagemagick \
        gifsicle \
        libarchive-tools \
        ca-certificates \
    && adduser -D -h /app -s /sbin/nologin bot

WORKDIR /app

COPY --from=builder /build/x_sticker_bot .

RUN mkdir -p /app/data && chown -R bot:bot /app

USER bot

VOLUME ["/app/data"]
EXPOSE 9090

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD wget -qO- http://localhost:9090/api/health || exit 1

ENTRYPOINT ["./x_sticker_bot"]
