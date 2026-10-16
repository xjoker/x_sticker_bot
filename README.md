# Telegram Sticker Downloader Bot

A Telegram bot that downloads individual stickers and sticker sets, converts animated media, and provides a small monitoring dashboard.

This is the primary English documentation. The [Chinese guide](README_CN.md) is maintained as a translation.

Try the public bot: [@x_sticker_bot](https://t.me/x_sticker_bot).

## Features

| Send or run | Result |
|---|---|
| A static sticker | A PNG file |
| An animated or video sticker | A GIF file |
| A sticker-set link such as `https://t.me/addstickers/example` | ZIP archive(s) with original and converted files |
| A Telegram animation message | A ZIP archive containing a converted GIF |
| `/info`, then a sticker | Sticker-set title, type, count, format counts, and link |
| `/start` or `/help` | Usage instructions |
| `/about` | Features, privacy details, version, and source repository |
| `/getfid` (configured admin only) | File ID for media in the message |

After downloading a sticker from a set, use the buttons to download the entire set or view its details. Set archives are split into volumes to keep them suitable for Telegram uploads. If some items fail, the bot sends the files it could process and reports the partial result.

The bot handles regular stickers, masks, and custom emoji stickers. TGS rendering requires `lottie_to_png`; TGS files with embedded image assets, animations longer than three seconds, or decompressed JSON larger than 5 MiB are not supported.

Other limits include one active download per user and up to 20 concurrent download tasks across the bot. The global limiter allows 100 updates per second with a burst of 200. Non-command messages and callbacks are limited to 30 per minute per user (burst 5). `/start` and `/help` share a per-user limit of 5 per minute (burst 2); `/about` and `/info` each have their own limit at the same rate.

## Run with Docker Compose

The recommended setup pulls the public `linux/amd64` image from GitHub Container Registry and stores runtime data in `./data` on the host.

```bash
git clone https://github.com/xjoker/x_sticker_bot.git
cd x_sticker_bot
cp .env.example .env
```

Edit `.env` and set `BOT_TOKEN`. Set `ADMIN_TOKEN` to protect the monitoring APIs and dashboard login. Then start the service:

```bash
docker compose up -d
docker compose logs -f bot
```

Open the dashboard at [http://127.0.0.1:9090/dashboard](http://127.0.0.1:9090/dashboard). Compose binds the monitoring port to loopback by default. For remote access, use an SSH tunnel or a reverse proxy with TLS and access controls.

To update to the latest image published by CI:

```bash
docker compose pull
docker compose up -d
```

The service writes its SQLite usage database and temporary work files under the mounted `./data` directory. `docker compose down` leaves that directory in place.

Compose limits the container to 512 MiB of memory and one CPU. Container logs rotate at 10 MiB per file, keeping three files.

The published image currently targets `linux/amd64`. To run on another architecture, build an image for that platform from source and verify the native dependencies there.

## Run from source

Use Go 1.27.2 or a compatible Go toolchain. The executable calls native tools for media conversion and archive handling:

| Tool | Used for |
|---|---|
| FFmpeg (`ffmpeg`) | GIF and WEBM conversion, including TGS output encoding |
| ImageMagick (`magick` on macOS/Windows; `convert` on Linux) | Static image conversion |
| libarchive (`bsdtar`) | Archive extraction |
| `lottie_to_png` | Rendering TGS animation frames; required for TGS stickers |
| `gifsicle` | Optional GIF optimization |

Install the tools with your operating system's package manager. On macOS, for example:

```bash
brew install ffmpeg imagemagick libarchive gifsicle cmake libpng
```

Build `lottie_to_png` from the pinned source revision when you need local TGS support:

```bash
git clone https://github.com/ed-asriyan/lottie-converter.git
cd lottie-converter
git checkout 1d4756c982be303d732bae3478b804ef74ec4825
cmake -S . -B build -DLOTTIE_MODULE=OFF -DCMAKE_BUILD_TYPE=Release -DCMAKE_POLICY_VERSION_MINIMUM=3.5
cmake --build build --target lottie_to_png
```

Put `build/bin/lottie_to_png` on `PATH`. Set `BOT_TOKEN` in your shell, return to the bot repository root, then build and run:

```bash
go build -o x_sticker_bot ./cmd/x_sticker_bot
./x_sticker_bot
```

If `BOT_TOKEN` is not set, the program exits during startup. Missing media tools are reported in the logs; the related conversions will not work until the tool is installed.

## Configuration

The executable accepts the flags below. Non-empty environment variables override flag values. The binary does not load `.env` files itself; Docker Compose loads `.env` through `env_file`.

| Environment variable | CLI flag | Default | Description |
|---|---|---|---|
| `BOT_TOKEN` | `-bot_token` | Required | Telegram Bot API token |
| `ADMIN_UID` | `-admin_uid` | `0` | Telegram user ID allowed to use `/getfid`; `0` leaves that command unavailable to normal users |
| `ADMIN_TOKEN` | `-admin_token` | Empty | Bearer token for the monitoring dashboard APIs; empty means no API authentication |
| `DATA_DIR` | `-data_dir` | `bot_data` | Directory for SQLite statistics and temporary files; Compose sets this to `/app/data` |
| `LOG_LEVEL` | `-log_level` | `info` | `debug`, `info`, `warn`, or `error` |
| `MONITOR_LISTEN_ADDR` | `-monitor_listen_addr` | `:9090` | Monitoring server listen address |
| `STATS_SALT` | — | Random per process | Stable secret used to hash user IDs; set it if anonymized user counts must remain consistent across restarts |

The `.env.example` file contains the Compose environment template. Keep the actual `.env` file private; it contains the bot token and may contain the dashboard token and statistics salt.

## Monitoring

| Endpoint | Access | Purpose |
|---|---|---|
| `/dashboard` | Public page; dashboard APIs require `ADMIN_TOKEN` when configured | Runtime and usage dashboard |
| `GET /api/health` | Always public | Polling health plus image version and Git commit |
| `GET /api/metrics` | Requires `ADMIN_TOKEN` when configured | Uptime, command/message/callback counts, rate-limit denials, memory, goroutines, and active tasks |
| `GET /api/stats` | Requires `ADMIN_TOKEN` when configured | Daily anonymized usage for the last 30 days and all-time totals |

The health response reports `starting` until the bot receives a successful `getUpdates` response, `ok` while polling remains fresh, and `polling_stale` after 130 seconds without success. The endpoint returns HTTP `503` for `starting` and `polling_stale`, and HTTP `200` for `ok`. The Docker health check uses this endpoint.

The dashboard page itself is served even when `ADMIN_TOKEN` is set; users enter the token in its sign-in form. The token stays in the current page's memory. Set `ADMIN_TOKEN` in production and keep the monitoring port private.

## Data and privacy

- The bot does not keep user sessions or sticker content in a database.
- SQLite stores daily usage counts. User IDs are salted, SHA-256 hashed, and truncated to 12 hexadecimal characters; raw IDs are not stored.
- When `STATS_SALT` is unset, a random salt is generated on every process start, so anonymized user counts are not consistent across restarts.
- Downloaded files are processed in temporary per-task directories and removed when processing finishes. A background cleanup removes abandoned managed directories older than 24 hours, checking once an hour.
- Logs go to stdout/stderr. Do not put tokens or personal information in logs.

## Development and CI

Run the checks used by CI before opening a change:

```bash
go test -race ./...
go vet ./...
go build -o /tmp/x_sticker_bot ./cmd/x_sticker_bot
```

GitHub Actions also runs `govulncheck`, builds the Docker image without cache, and converts a TGS fixture inside the image. Pull requests and pushes run checks; a successful push to `main` publishes both `ghcr.io/xjoker/x_sticker_bot:<version>` and `ghcr.io/xjoker/x_sticker_bot:latest`.

The version comes from the root `VERSION` file in `YYYYMMDD.N` format. CI injects that version and the source commit into the binary, image labels, and `/api/health` response. The current GHCR package is [xjoker/x_sticker_bot](https://github.com/xjoker/x_sticker_bot/pkgs/container/x_sticker_bot).

## Project layout

```text
cmd/x_sticker_bot/       Application entry point
internal/bot/            Long polling, update routing, and application wiring
internal/command/        Telegram commands and download flows
internal/config/         Flags and environment configuration
internal/message/        User-facing English and Chinese messages
internal/monitor/        Dashboard, metrics, and polling health
internal/ratelimit/      Global, per-user, and per-command rate limits
internal/sticker/        Conversion, TGS rendering, and archive helpers
internal/storage/        Temporary-file management and SQLite statistics
```

## License

MIT. See [LICENSE](LICENSE).
