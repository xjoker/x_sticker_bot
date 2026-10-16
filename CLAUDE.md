# Repository guide

The primary project documentation is the English [README](README.md); [README_CN.md](README_CN.md) is its Chinese translation.

## Project

Telegram sticker downloader written in Go 1.27.2. The bot uses long polling and has no user session store. SQLite is used only for anonymized usage statistics.

## Build and run

```bash
go build -o x_sticker_bot ./cmd/x_sticker_bot
go test -race ./...
go vet ./...
```

Set `BOT_TOKEN` in the shell before running `./x_sticker_bot`. The binary reads flags and environment variables; non-empty environment values take precedence. It does not load `.env` files. Docker Compose loads `.env` from the project root.

## Configuration and storage

See [`internal/config/config.go`](internal/config/config.go) and [`.env.example`](.env.example) for the configuration surface. Runtime settings are supplied through flags and environment variables; `STATS_SALT` is read by the stats package.

The default data directory is `bot_data`; Compose sets `/app/data` and mounts it to `./data`. SQLite stores daily statistics with salted, truncated hashes of user IDs. If `STATS_SALT` is unset, the process generates a random salt at startup, so hashes do not remain stable across restarts. Temporary task directories are removed after use; a cleanup loop purges managed directories older than 24 hours every hour.

## Runtime dependencies

The converter detects required executables at startup and warns when tools are missing:

- FFmpeg (`ffmpeg`) handles video and GIF conversion.
- ImageMagick uses `magick` on macOS/Windows and `convert` on Linux.
- libarchive provides `bsdtar` for archive extraction.
- `lottie_to_png` renders TGS sticker frames; the Docker image builds it from the pinned `ed-asriyan/lottie-converter` revision.
- `gifsicle` is optional and optimizes GIF output on a best-effort basis.

## Application behavior

`cmd/x_sticker_bot` configures logging and wires `bot.App` to `command.Handler`. `internal/bot` registers `/start`, `/help`, `/about`, `/info`, and `/getfid`, routes updates, and tracks polling health. `/getfid` is restricted by `ADMIN_UID`.

Sending a sticker returns a PNG for static stickers or a GIF for animated/video stickers. A sticker-set link downloads the original and converted files into ZIP volumes; inline buttons also start a whole-set download or show set information. Telegram animation messages return a ZIP containing a converted GIF. `/info` prompts the user to send a sticker, and the sticker-info callback reports set metadata.

TGS conversion renders 256×256 frames at 15 fps, supports animations up to three seconds, and rejects embedded image assets and decompressed JSON over 5 MiB. Download tasks are limited to one per user and 20 globally.

## Monitoring

Gin serves `/dashboard`, `/api/health`, `/api/metrics`, and `/api/stats` on `MONITOR_LISTEN_ADDR` (default `:9090`). `ADMIN_TOKEN` protects metrics and stats APIs; `/api/health` remains public. Health is `starting` before the first valid `getUpdates` response, `ok` while polling is fresh, and `polling_stale` after 130 seconds without success. The health response also includes `version` and `git_commit`.

## Container and CI

The multi-stage Docker build includes the native media tools and TGS renderer. The public image is hosted at `ghcr.io/xjoker/x_sticker_bot`; Compose binds the dashboard to `127.0.0.1:9090`, persists `/app/data` in `./data`, and limits the container to 512 MiB of memory and one CPU. Container logs rotate at 10 MiB per file, keeping three files. The current published image targets `linux/amd64`.

The root `VERSION` file is the version source (`YYYYMMDD.N`). GitHub Actions runs `govulncheck`, race tests, `go vet`, a Go build, a no-cache container build, and a TGS conversion check. Successful pushes to `main` publish the version tag and `latest` to GHCR; pull requests run validation without publishing.

## Package layout

- `internal/bot`: application wiring, Telegram long polling, update routing.
- `internal/command`: command handling and file download flows.
- `internal/config`: flags and environment parsing.
- `internal/message`: bilingual user-facing messages.
- `internal/monitor`: dashboard, metrics, and polling health.
- `internal/ratelimit`: global, per-user, and per-command throttling.
- `internal/sticker`: media conversion, TGS rendering, archives.
- `internal/storage`: temporary directories and SQLite stats.
