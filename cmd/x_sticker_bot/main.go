package main

import (
	"fmt"
	"log/slog"
	"os"

	appbot "github.com/xjoker/x_sticker_bot/internal/bot"
	"github.com/xjoker/x_sticker_bot/internal/command"
	"github.com/xjoker/x_sticker_bot/internal/config"
)

func main() {
	cfg := config.Parse()
	if cfg.BotToken == "" {
		fmt.Fprintln(os.Stderr, "BOT_TOKEN is required")
		os.Exit(1)
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		fmt.Fprintf(os.Stderr, "invalid LOG_LEVEL %q: %v\n", cfg.LogLevel, err)
		os.Exit(1)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	if err := appbot.Run(cfg, newCommandHandler); err != nil {
		slog.Error("bot stopped with error", "error", err)
		os.Exit(1)
	}
}

func newCommandHandler(app *appbot.App) appbot.CommandHandler {
	h := &command.Handler{
		Bot:       app.Bot,
		TempMgr:   app.TempMgr,
		Stats:     app.Stats,
		Metrics:   app.Metrics,
		BotName:   app.BotName,
		BotToken:  app.Config.BotToken,
		AdminUID:  app.Config.AdminUID,
		GlobalSem: make(chan struct{}, command.MaxGlobalTasks),
	}
	app.ActiveTasksFn = func() (int, int) {
		return len(h.GlobalSem), cap(h.GlobalSem)
	}
	return h
}
