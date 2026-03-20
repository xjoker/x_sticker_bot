# Telegram 贴纸下载机器人

一个轻量的 Telegram 贴纸下载机器人，发送贴纸即可获取 PNG/GIF 文件。

**立即体验:** [@x_sticker_bot](https://t.me/x_sticker_bot)

[English](README.md)

## 功能

- **发送贴纸** → 立即获取 PNG（静态）或 GIF（动态/视频）
- **发送贴纸包链接** → 下载整个贴纸包为 ZIP
- **发送 GIF** → 转换并下载
- **/info** → 查看贴纸包详情（标题、类型、数量、链接）
- **并发控制** — 全局最多 20 个任务，每用户 1 个
- **三级限流** — 全局 + 命令级 + 用户级令牌桶
- **匿名统计** — SHA256 哈希用户 ID，不存储原始数据
- **监控面板** — 内置 Web UI，端口 `:9090/dashboard`

## 运行依赖

通过 `exec.Command` 调用的外部工具：

| 工具 | 用途 | 必需 |
|---|---|---|
| ffmpeg | 视频/动画 → GIF 转换 | 是 |
| ImageMagick | 图片 → PNG 转换 | 是 |
| bsdtar | 压缩包解压 / ZIP 打包 | 是 |
| gifsicle | GIF 优化 | 否（可选） |

## 快速开始

### 本地运行

```bash
# macOS 安装依赖
brew install ffmpeg imagemagick gifsicle libarchive

# 构建运行
go build -o x_sticker_bot ./cmd/x_sticker_bot
BOT_TOKEN="YOUR_TOKEN" ./x_sticker_bot
```

### Docker 部署（推荐）

```bash
cp .env.example .env
# 编辑 .env 填写 BOT_TOKEN

docker compose up -d
docker compose logs -f
```

支持 `linux/amd64` 和 `linux/arm64` 双平台。

## 配置

所有参数支持 CLI flag 和环境变量，环境变量优先。

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `BOT_TOKEN` | *（必填）* | Telegram Bot API Token |
| `ADMIN_UID` | `0` | 管理员用户 ID（用于 /getfid 命令） |
| `ADMIN_TOKEN` | | 监控面板 Bearer Token |
| `DATA_DIR` | `bot_data` | 数据目录（临时文件 + stats.db） |
| `LOG_LEVEL` | `info` | 日志级别：debug, info, warn, error |
| `STATS_SALT` | *（随机生成）* | 匿名统计盐值（持久化部署建议设置） |
| `MONITOR_LISTEN_ADDR` | `:9090` | 监控面板监听地址 |

## 监控

- 面板：`http://host:9090/dashboard`
- 健康检查：`GET /api/health`（始终公开）
- 指标 API：`GET /api/metrics`（设置 `ADMIN_TOKEN` 后需认证）
- 统计 API：`GET /api/stats`（设置 `ADMIN_TOKEN` 后需认证）

**安全提示：** 生产环境务必设置 `ADMIN_TOKEN`，并将 9090 端口绑定到 `127.0.0.1`（docker-compose 默认已配置）。

## 隐私

- 仅存储匿名使用统计（SHA256 哈希用户 ID）
- 不保留原始用户 ID、消息内容或贴纸文件
- 贴纸文件仅临时处理后立即删除
- 临时文件每小时自动清理（最长保留 24 小时）

## 项目结构

```
cmd/x_sticker_bot/       入口
internal/
├── bot/                  App 容器、事件路由
├── command/              命令处理（下载、info）
├── sticker/              贴纸处理（转换、压缩）
├── storage/              临时文件管理 + 匿名统计（SQLite）
├── config/               配置解析（flag + 环境变量）
├── message/              消息模板（中英双语）
├── ratelimit/            三级限流
└── monitor/              Web 监控面板
```

## 许可证

MIT
