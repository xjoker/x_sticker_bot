# Telegram Sticker Downloader Bot

A lightweight Telegram bot for downloading stickers as PNG/GIF files.

[中文文档](README_CN.md)

## Features

- **Send a sticker** → instantly get PNG (static) or GIF (animated/video)
- **Send a sticker set link** → download the entire set as ZIP
- **Send a GIF** → convert and download
- **/info** → view sticker set details (title, type, count, link)
- **Concurrency control** — 20 global tasks max, 1 per user
- **Rate limiting** — global + per-command + per-user token bucket
- **Anonymous stats** — SHA256-hashed user IDs, no raw PII stored
- **Monitoring dashboard** — built-in web UI at `:9090/dashboard`

## Requirements

Runtime dependencies (called via `exec.Command`):

| Tool | Purpose | Required |
|---|---|---|
| ffmpeg | Video/animation → GIF conversion | Yes |
| ImageMagick | Image → PNG conversion | Yes |
| bsdtar | Archive extraction / ZIP compression | Yes |
| gifsicle | GIF optimization | No (optional) |

## Quick Start

### Local

```bash
# macOS
brew install ffmpeg imagemagick gifsicle libarchive

# Build & run
go build -o x_sticker_bot ./cmd/x_sticker_bot
BOT_TOKEN="YOUR_TOKEN" ./x_sticker_bot
```

### Docker (recommended)

```bash
cp .env.example .env
# Edit .env and set BOT_TOKEN

docker compose up -d
docker compose logs -f
```

Multi-platform support: `linux/amd64` and `linux/arm64`.

## Configuration

All parameters support CLI flags and environment variables. Env vars take priority.

| Env Var | Default | Description |
|---|---|---|
| `BOT_TOKEN` | *(required)* | Telegram Bot API Token |
| `ADMIN_UID` | `0` | Admin user ID (for /getfid command) |
| `ADMIN_TOKEN` | | Bearer token for monitoring dashboard |
| `DATA_DIR` | `bot_data` | Data directory (temp files + stats.db) |
| `LOG_LEVEL` | `info` | Log level: debug, info, warn, error |
| `STATS_SALT` | *(random)* | Salt for anonymized stats (set for persistent deployments) |
| `MONITOR_LISTEN_ADDR` | `:9090` | Monitoring dashboard listen address |

## Monitoring

- Dashboard: `http://host:9090/dashboard`
- Health check: `GET /api/health` (always public)
- Metrics API: `GET /api/metrics` (behind auth if `ADMIN_TOKEN` set)
- Stats API: `GET /api/stats` (behind auth if `ADMIN_TOKEN` set)

**Security:** In production, always set `ADMIN_TOKEN` and bind port 9090 to `127.0.0.1` (default in docker-compose).

## Privacy

- Only anonymized usage statistics are stored (SHA256-hashed user IDs)
- No raw user IDs, messages, or sticker content is retained
- Sticker files are processed temporarily and deleted immediately
- Temp files are auto-cleaned every hour (24h max age)

## Project Structure

```
cmd/x_sticker_bot/       Entry point
internal/
├── bot/                  App container, event routing
├── command/              Command handlers (download, info)
├── sticker/              Sticker processing (convert, compress)
├── storage/              Temp file manager + anonymous stats (SQLite)
├── config/               Config parsing (flags + env vars)
├── message/              Message templates (EN + CN bilingual)
├── ratelimit/            Three-tier rate limiting
└── monitor/              Web monitoring dashboard
```

## License

MIT
