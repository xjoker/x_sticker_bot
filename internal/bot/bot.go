package bot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/xjoker/x_sticker_bot/internal/config"
	"github.com/xjoker/x_sticker_bot/internal/monitor"
	"github.com/xjoker/x_sticker_bot/internal/ratelimit"
	"github.com/xjoker/x_sticker_bot/internal/sticker"
	"github.com/xjoker/x_sticker_bot/internal/storage"
)

const BotVersion = "1.0.0"

// App is the main application container holding all dependencies.
type App struct {
	Bot     *tgbot.Bot
	Config  *config.Config
	Limiter *ratelimit.RateLimiter
	Metrics *monitor.Metrics
	TempMgr *storage.TempManager
	Stats   *storage.Stats
	BotName string

	cmdHandler     CommandHandler
	ActiveTasksFn  func() (int, int) // returns (active, capacity) for task monitoring
}

// CommandHandler is the interface the command package must implement.
type CommandHandler interface {
	CmdStart(ctx context.Context, b *tgbot.Bot, update *models.Update)
	CmdAbout(ctx context.Context, b *tgbot.Bot, update *models.Update)
	CmdInfo(ctx context.Context, b *tgbot.Bot, update *models.Update)
	CmdGetFID(ctx context.Context, b *tgbot.Bot, update *models.Update)
	HandleMessage(ctx context.Context, b *tgbot.Bot, update *models.Update)
}

// CommandHandlerFactory creates a CommandHandler from an initialized App.
type CommandHandlerFactory func(app *App) CommandHandler

// Run initializes and starts the bot. Blocks until interrupted.
func Run(cfg *config.Config, factory CommandHandlerFactory) error {
	slog.Info("starting x_sticker_bot", "version", BotVersion)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	app := &App{Config: cfg}

	if cfg.AdminUID == 0 {
		slog.Info("ADMIN_UID not set, /getfid command is disabled")
	}

	// Initialize metrics
	app.Metrics = monitor.NewMetrics()

	// Initialize rate limiter
	app.Limiter = ratelimit.New(100, 200)
	app.Limiter.SetCommandRate("/start", 5.0/60, 2)
	app.Limiter.SetCommandRate("/about", 5.0/60, 2)

	// Initialize temp file manager
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = "bot_data"
	}
	tmpMgr, err := storage.NewTempManager(dataDir)
	if err != nil {
		return fmt.Errorf("failed to init temp manager: %w", err)
	}
	app.TempMgr = tmpMgr

	// Initialize stats database
	statsPath := filepath.Join(dataDir, "stats.db")
	stats, err := storage.NewStats(statsPath)
	if err != nil {
		slog.Warn("stats database init failed, running without stats", "error", err)
	} else {
		app.Stats = stats
		defer stats.Close()
	}

	// Start auto-cleanup for temp files
	cleanupStop := make(chan struct{})
	defer close(cleanupStop)
	app.TempMgr.StartAutoClean(1*time.Hour, 24*time.Hour, cleanupStop)

	// Start rate limiter cleanup
	limiterStop := make(chan struct{})
	defer close(limiterStop)
	app.Limiter.StartCleanup(10*time.Minute, 30*time.Minute, limiterStop)

	// Initialize sticker conversion tools
	sticker.InitConvert()
	if missing := sticker.CheckDeps(); len(missing) > 0 {
		slog.Warn("missing external dependencies", "missing", missing)
	}

	// Initialize sticker worker pools
	if err := sticker.InitWorkers(); err != nil {
		return fmt.Errorf("failed to init sticker workers: %w", err)
	}
	defer sticker.CloseWorkers()

	// Create bot
	b, err := tgbot.New(cfg.BotToken, tgbot.WithDefaultHandler(app.defaultHandler))
	if err != nil {
		return fmt.Errorf("failed to create bot: %w", err)
	}
	app.Bot = b

	// Get bot info
	me, err := b.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("failed to get bot info: %w", err)
	}
	app.BotName = me.Username
	slog.Info("bot authorized", "username", app.BotName)

	// Wire command handler
	if factory != nil {
		app.cmdHandler = factory(app)
	}

	// Set bot profile and commands in background (non-blocking)
	go func() {
		if _, err := b.SetMyShortDescription(ctx, &tgbot.SetMyShortDescriptionParams{
			ShortDescription: "Telegram sticker downloader. Send any sticker to get PNG/GIF instantly.\n" +
				"Telegram 贴纸下载器，发送贴纸即可获取 PNG/GIF。",
		}); err != nil {
			slog.Warn("failed to set bot short description", "error", err)
		}
		if _, err := b.SetMyDescription(ctx, &tgbot.SetMyDescriptionParams{
			Description: "A simple bot to download Telegram stickers.\n\n" +
				"📥 Send a sticker → get PNG/GIF instantly\n" +
				"📦 Send a sticker set link → download the whole set\n" +
				"🎨 Send a GIF → convert and download\n\n" +
				"一个简单的 Telegram 贴纸下载机器人。\n\n" +
				"📥 发送贴纸 → 立即获取 PNG/GIF\n" +
				"📦 发送贴纸包链接 → 下载整个贴纸包\n" +
				"🎨 发送 GIF → 转换并下载",
		}); err != nil {
			slog.Warn("failed to set bot description", "error", err)
		}
		if _, err := b.SetMyCommands(ctx, &tgbot.SetMyCommandsParams{
			Commands: []models.BotCommand{
				{Command: "start", Description: "Usage / 使用说明"},
				{Command: "info", Description: "Sticker set info / 贴纸包详情"},
				{Command: "about", Description: "About / 关于"},
			},
		}); err != nil {
			slog.Warn("failed to set bot commands", "error", err)
		}
	}()

	// Register command handlers (MatchTypeCommand expects pattern WITHOUT leading '/')
	startHandler := app.withRateLimit("/start", app.routeCommand)
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "start", tgbot.MatchTypeCommand, startHandler)
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "help", tgbot.MatchTypeCommand, startHandler) // shares /start bucket
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "about", tgbot.MatchTypeCommand, app.withRateLimit("/about", app.routeCommand))
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "info", tgbot.MatchTypeCommand, app.routeCommand)
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "getfid", tgbot.MatchTypeCommand, app.routeCommand)

	// Start monitoring dashboard
	if cfg.MonitorListenAddr != "" {
		if cfg.AdminToken == "" {
			slog.Warn("ADMIN_TOKEN is not set — monitoring dashboard has no authentication")
		}
		go app.startMonitorServer(cfg)
	}

	slog.Info("bot started, polling for updates")
	b.Start(ctx)

	slog.Info("bot stopped")
	return nil
}

// routeCommand dispatches registered command messages to the appropriate handler.
func (app *App) routeCommand(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil || app.cmdHandler == nil {
		return
	}

	cmd := extractCommand(update.Message.Text)
	app.Metrics.RecordRequest(cmd)

	switch cmd {
	case "/start", "/help":
		app.cmdHandler.CmdStart(ctx, b, update)
	case "/about":
		app.cmdHandler.CmdAbout(ctx, b, update)
	case "/info":
		app.cmdHandler.CmdInfo(ctx, b, update)
	case "/getfid":
		app.cmdHandler.CmdGetFID(ctx, b, update)
	}
}

// defaultHandler handles all non-command messages and callbacks.
func (app *App) defaultHandler(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if app.cmdHandler == nil {
		return
	}

	// Handle callback queries
	if update.CallbackQuery != nil {
		uid := update.CallbackQuery.From.ID
		if !app.Limiter.Allow(uid) {
			app.Metrics.RecordRateDenied()
			return
		}
		app.Metrics.RecordRequest("callback")
		app.cmdHandler.HandleMessage(ctx, b, update)
		return
	}

	if update.Message == nil {
		return
	}

	uid := update.Message.From.ID
	if !app.Limiter.Allow(uid) {
		app.Metrics.RecordRateDenied()
		return
	}

	app.Metrics.RecordRequest("message")
	app.cmdHandler.HandleMessage(ctx, b, update)
}

// withRateLimit wraps a handler with per-command rate limiting.
func (app *App) withRateLimit(cmd string, handler tgbot.HandlerFunc) tgbot.HandlerFunc {
	return func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
		if update.Message == nil {
			return
		}
		uid := update.Message.From.ID
		if !app.Limiter.AllowCommand(uid, cmd) {
			app.Metrics.RecordRateDenied()
			slog.Debug("rate limited", "uid", uid, "command", cmd)
			b.SendMessage(ctx, &tgbot.SendMessageParams{
				ChatID: update.Message.Chat.ID,
				Text:   "Too many requests. Please wait.\n請求過於頻繁，請稍後再試。",
			})
			return
		}
		handler(ctx, b, update)
	}
}

// extractCommand extracts the command from message text (e.g., "/start@botname" → "/start").
func extractCommand(text string) string {
	if text == "" || text[0] != '/' {
		return ""
	}
	cmd := text
	if idx := strings.IndexByte(cmd, '@'); idx >= 0 {
		cmd = cmd[:idx]
	}
	if idx := strings.IndexByte(cmd, ' '); idx >= 0 {
		cmd = cmd[:idx]
	}
	return cmd
}

// startMonitorServer starts the Gin monitoring dashboard server.
func (app *App) startMonitorServer(cfg *config.Config) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	var statsProvider monitor.StatsProvider
	if app.Stats != nil {
		statsProvider = app.Stats
	}
	monitor.RegisterRoutes(r, app.Metrics, cfg.AdminToken, statsProvider, app.ActiveTasksFn)

	slog.Info("monitoring dashboard started", "addr", cfg.MonitorListenAddr)
	if err := r.Run(cfg.MonitorListenAddr); err != nil {
		slog.Error("monitor server failed", "error", err)
	}
}
