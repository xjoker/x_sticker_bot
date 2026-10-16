# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**Telegram Sticker Downloader Bot** — Download Telegram stickers as PNG/GIF. Written in Go 1.27.2, uses long-polling.

## Build & Run

```bash
go build -o x_sticker_bot ./cmd/x_sticker_bot
BOT_TOKEN="TOKEN" ./sticker-bot

go vet ./...

docker compose up -d
```

## Configuration

All params support CLI flags and env vars. Env vars override flags. See `internal/config/config.go`. Template at `.env.example`.

## External Runtime Dependencies

Called via `exec.Command` (detection in `internal/sticker/convert.go:InitConvert()`):
- **ffmpeg** — video/animation → WEBM/GIF conversion
- **ImageMagick** (`magick` on macOS, `convert` on Linux) — image → PNG/WEBP conversion
- **bsdtar** (`libarchive-tools`) — archive extraction and ZIP compression
- **gifsicle** — GIF optimization (optional, best-effort)

## Architecture

### No Sessions, No Database (for user data)

This is a stateless download bot. No session system, no user data persistence. SQLite is only used for anonymized usage statistics (`storage/stats.go`).

### Command Handler Interface

`bot.App` → `CommandHandler` interface → `command.Handler`. Wired via factory in `main.go` to avoid circular imports.

### Download Flow

`HandleMessage` in `download.go` dispatches by content type:
- Sticker → `HandleDownload` (single) + offer "download whole set" button
- Link → `HandleDownload` (whole set)
- Animation → `handleAnimationDownload` (convert to GIF)
- Callback `CbDnWhole` → download whole set
- Callback `CbStickerInfo` → `HandleStickerInfo`

### Sticker Processing

`internal/sticker/`:
- `convert.go` — format detection and conversion (static→PNG/WEBP, video→GIF/WEBM)
- `download.go` — download stickers from Telegram, zip entire sets
- `workers.go` — `ants` goroutine pool (8 workers) for parallel downloads
- `util.go` — archive extraction, compression

### Rate Limiting

Three-tier in `internal/ratelimit/`: global (100 req/s), per-command, per-user token bucket.

### Monitoring

Gin dashboard on `:9090`. Shows uptime, memory, request rate, command stats, daily usage stats.

### Anonymized Stats

`storage/stats.go` — SQLite, user ID hashed (SHA256 + salt, truncated to 12 hex chars). Tracks daily unique users and download counts.
