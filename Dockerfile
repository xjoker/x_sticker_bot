FROM golang:1.27.2-alpine AS builder

RUN apk add --no-cache git

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o x_sticker_bot ./cmd/x_sticker_bot

FROM alpine:3.21 AS lottie-builder

RUN apk add --no-cache build-base cmake git libpng-dev libpng-static zlib-dev zlib-static
RUN git clone https://github.com/ed-asriyan/lottie-converter.git /build/lottie-converter \
    && cd /build/lottie-converter \
    && git checkout 1d4756c982be303d732bae3478b804ef74ec4825 \
    && cmake -S . -B build -DLOTTIE_MODULE=OFF -DCMAKE_BUILD_TYPE=Release -DCMAKE_POLICY_VERSION_MINIMUM=3.5 \
    && cmake --build build --target lottie_to_png

RUN cd /build/lottie-converter \
    && mkdir -p /licenses/lottie-converter \
    && cp LICENSE /licenses/lottie-converter/lottie-converter.LICENSE \
    && cp build/_deps/rlottie-src/COPYING /licenses/lottie-converter/rlottie.COPYING \
    && cp build/_deps/argparse-src/LICENSE /licenses/lottie-converter/argparse.LICENSE

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
COPY --from=lottie-builder /build/lottie-converter/build/bin/lottie_to_png /usr/local/bin/lottie_to_png
COPY --from=lottie-builder /licenses/lottie-converter/ /usr/share/licenses/lottie-converter/

RUN mkdir -p /app/data && chown -R bot:bot /app

USER bot

VOLUME ["/app/data"]
EXPOSE 9090

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD wget -qO- http://localhost:9090/api/health || exit 1

ENTRYPOINT ["./x_sticker_bot"]
