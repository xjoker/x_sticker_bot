# Telegram 贴纸下载机器人

Telegram 机器人，可下载单张贴纸或整套贴纸、转换动态媒体，并提供监控面板。

本仓库以 [英文 README](README.md) 为主文档；此页为中文译本。

体验机器人：[@x_sticker_bot](https://t.me/x_sticker_bot)。

## 功能

| 发送或执行 | 结果 |
|---|---|
| 静态贴纸 | PNG 文件 |
| 动态或视频贴纸 | GIF 文件 |
| 贴纸包链接，例如 `https://t.me/addstickers/example` | 包含原始文件和转换文件的 ZIP 压缩包，可分卷发送 |
| Telegram 动画消息 | ZIP 压缩包，内含转换后的 GIF |
| `/info` 后发送贴纸 | 贴纸包标题、类型、数量、格式统计和链接 |
| `/start` 或 `/help` | 使用说明 |
| `/about` | 功能、隐私、版本和源码仓库信息 |
| 管理员执行 `/getfid` 并发送媒体 | 返回媒体的 Telegram 文件 ID |

下载贴纸包中的单张贴纸后，可点击按钮下载整包或查看详情。压缩包会按卷拆分，以适应 Telegram 的上传限制。若部分文件处理失败，机器人会发送可用文件并提示不完整结果。

机器人支持普通贴纸、面具贴纸和自定义 Emoji 贴纸。TGS 渲染需要 `lottie_to_png`；包含嵌入图片资源、时长超过 3 秒或解压后的 JSON 超过 5 MiB 的 TGS 文件不受支持。

每位用户同一时间只能有一个下载任务；机器人全局最多同时处理 20 个下载任务。全局限流为每秒 100 个 Update（突发 200 个）。非命令消息和回调每位用户每分钟最多 30 次（突发 5 次）。`/start` 和 `/help` 共用每位用户每分钟 5 次的限流桶（突发 2 次）；`/about` 和 `/info` 各自使用相同速率的独立限流桶。

## 使用 Docker Compose

推荐使用从 GitHub Container Registry 拉取的公开 `linux/amd64` 镜像。SQLite 统计数据库和临时文件保存在宿主机的 `./data` 目录。

```bash
git clone https://github.com/xjoker/x_sticker_bot.git
cd x_sticker_bot
cp .env.example .env
```

编辑 `.env` 并填写 `BOT_TOKEN`。设置 `ADMIN_TOKEN` 可保护监控 API 并启用面板登录。然后启动服务：

```bash
docker compose up -d
docker compose logs -f bot
```

打开 [http://127.0.0.1:9090/dashboard](http://127.0.0.1:9090/dashboard) 查看面板。Compose 默认只将监控端口绑定到本机回环地址。远程访问请使用 SSH 隧道，或配置带 TLS 和访问控制的反向代理。

更新到 CI 最近发布的镜像：

```bash
docker compose pull
docker compose up -d
```

`docker compose down` 不会删除 `./data` 目录。

Compose 将容器内存限制为 512 MiB、CPU 限制为 1 核。容器日志按每个文件 10 MiB 轮转，最多保留 3 个文件。

当前发布镜像仅支持 `linux/amd64`。其他架构需要自行构建，并验证对应平台的原生依赖。

## 从源码运行

使用 Go 1.27.2 或兼容版本。程序会调用下列原生工具处理媒体和压缩包：

| 工具 | 用途 |
|---|---|
| FFmpeg（`ffmpeg`） | GIF 和 WEBM 转换，包括 TGS 转 GIF 的编码 |
| ImageMagick（macOS/Windows 使用 `magick`；Linux 使用 `convert`） | 静态图片转换 |
| libarchive（`bsdtar`） | 解压归档文件 |
| `lottie_to_png` | 渲染 TGS 动画帧；处理 TGS 时必需 |
| `gifsicle` | 可选的 GIF 优化工具 |

请用操作系统的包管理器安装工具。macOS 示例：

```bash
brew install ffmpeg imagemagick libarchive gifsicle cmake libpng
```

如需本地处理 TGS，请从固定源码版本构建 `lottie_to_png`：

```bash
git clone https://github.com/ed-asriyan/lottie-converter.git
cd lottie-converter
git checkout 1d4756c982be303d732bae3478b804ef74ec4825
cmake -S . -B build -DLOTTIE_MODULE=OFF -DCMAKE_BUILD_TYPE=Release -DCMAKE_POLICY_VERSION_MINIMUM=3.5
cmake --build build --target lottie_to_png
```

将 `build/bin/lottie_to_png` 放入 `PATH`。在 shell 中设置 `BOT_TOKEN`，返回机器人仓库根目录，再构建和运行：

```bash
go build -o x_sticker_bot ./cmd/x_sticker_bot
./x_sticker_bot
```

运行前请在 shell 中设置 `BOT_TOKEN`。未设置时程序会在启动阶段退出。缺少媒体工具会写入日志，对应转换功能无法使用，直到安装该工具。

## 配置

程序支持下表中的命令行参数。非空环境变量优先于命令行参数。二进制程序本身不会读取 `.env`；Docker Compose 通过 `env_file` 载入该文件。

| 环境变量 | 命令行参数 | 默认值 | 说明 |
|---|---|---|---|
| `BOT_TOKEN` | `-bot_token` | 必填 | Telegram Bot API Token |
| `ADMIN_UID` | `-admin_uid` | `0` | 可使用 `/getfid` 的 Telegram 用户 ID；`0` 时普通用户无法使用此命令 |
| `ADMIN_TOKEN` | `-admin_token` | 空 | 监控面板 API 的 Bearer Token；留空时 API 不做认证 |
| `DATA_DIR` | `-data_dir` | `bot_data` | SQLite 统计库和临时文件目录；Compose 中设为 `/app/data` |
| `LOG_LEVEL` | `-log_level` | `info` | `debug`、`info`、`warn` 或 `error` |
| `MONITOR_LISTEN_ADDR` | `-monitor_listen_addr` | `:9090` | 监控服务监听地址 |
| `STATS_SALT` | — | 每次进程启动时随机生成 | 用于哈希用户 ID 的稳定密钥；需跨重启保持匿名用户统计一致时请设置 |

`.env.example` 是 Compose 环境模板。请妥善保护实际的 `.env` 文件，其中包含机器人 Token，也可能包含面板 Token 和统计盐值。

## 监控

| 地址 | 访问控制 | 用途 |
|---|---|---|
| `/dashboard` | 页面可访问；设置 `ADMIN_TOKEN` 后，面板 API 需要认证 | 运行状态和使用统计面板 |
| `GET /api/health` | 始终公开 | 轮询健康状态、镜像版本和 Git 提交号 |
| `GET /api/metrics` | 设置 `ADMIN_TOKEN` 后需要认证 | 运行时间、命令、消息和回调计数、限流拒绝数、内存、协程和活动任务 |
| `GET /api/stats` | 设置 `ADMIN_TOKEN` 后需要认证 | 最近 30 天匿名统计及累计数据 |

健康状态为：首次收到成功的 `getUpdates` 响应前是 `starting`；轮询持续正常时是 `ok`；连续 130 秒没有成功响应时是 `polling_stale`。`starting` 和 `polling_stale` 返回 HTTP `503`，`ok` 返回 HTTP `200`。Docker 健康检查使用此接口。

设置 `ADMIN_TOKEN` 后，面板页面仍可打开，用户在登录框输入 Token。Token 只保存在当前页面内存中。生产环境请设置 `ADMIN_TOKEN` 并限制监控端口的网络访问。

## 数据与隐私

- 机器人不建立用户会话，也不会把贴纸内容存入数据库。
- SQLite 只保存每日使用统计。用户 ID 使用盐值和 SHA-256 哈希处理，并截断为 12 位十六进制字符；不会保存原始 ID。
- 未设置 `STATS_SALT` 时，每次进程启动都会生成新盐值，因此不同重启之间的匿名用户统计无法保持一致。
- 下载文件在每个任务的临时目录中处理，完成后删除。后台清理程序每小时检查一次，并删除超过 24 小时的遗留目录。
- 日志写入 stdout/stderr。不要在日志中记录 Token 或个人信息。

## 开发与 CI

提交变更前可运行 CI 使用的 Go 检查：

```bash
go test -race ./...
go vet ./...
go build -o /tmp/x_sticker_bot ./cmd/x_sticker_bot
```

GitHub Actions 还会运行 `govulncheck`、无缓存构建 Docker 镜像，并在镜像中转换 TGS 测试文件。Pull request 和分支推送会运行检查；`main` 上的推送在全部检查通过后，会发布 `ghcr.io/xjoker/x_sticker_bot:<version>` 和 `ghcr.io/xjoker/x_sticker_bot:latest`。

镜像版本来自仓库根目录的 `VERSION` 文件，格式为 `YYYYMMDD.N`。CI 会将版本和源码提交号写入二进制、镜像标签及 `/api/health` 响应。当前 GHCR 镜像包：[xjoker/x_sticker_bot](https://github.com/xjoker/x_sticker_bot/pkgs/container/x_sticker_bot)。

## 项目结构

```text
cmd/x_sticker_bot/       程序入口
internal/bot/            长轮询、Update 路由和应用装配
internal/command/        Telegram 命令和下载流程
internal/config/         命令行参数和环境变量配置
internal/message/        中英文用户消息
internal/monitor/        面板、指标和轮询健康状态
internal/ratelimit/      全局、用户级和命令级限流
internal/sticker/        格式转换、TGS 渲染和归档工具
internal/storage/        临时文件管理和 SQLite 统计
```

## 许可证

MIT，详见 [LICENSE](LICENSE)。
