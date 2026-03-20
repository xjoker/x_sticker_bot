package config

import (
	"flag"
	"os"
	"strconv"
)

type Config struct {
	BotToken          string
	AdminUID          int64
	AdminToken        string // Bearer token for monitoring dashboard
	DataDir           string
	LogLevel          string
	MonitorListenAddr string
}

func Parse() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.BotToken, "bot_token", "", "Telegram Bot API token (required)")
	flag.Int64Var(&cfg.AdminUID, "admin_uid", 0, "Admin user ID for privileged operations")
	flag.StringVar(&cfg.AdminToken, "admin_token", "", "Bearer token for monitoring dashboard")
	flag.StringVar(&cfg.DataDir, "data_dir", "", "Working directory for temporary files (default: bot_data)")
	flag.StringVar(&cfg.LogLevel, "log_level", "info", "Log level: debug, info, warn, error")
	flag.StringVar(&cfg.MonitorListenAddr, "monitor_listen_addr", ":9090", "Monitoring dashboard listen address")

	flag.Parse()

	// Environment variables override flags.
	envStr("BOT_TOKEN", &cfg.BotToken)
	envInt64("ADMIN_UID", &cfg.AdminUID)
	envStr("ADMIN_TOKEN", &cfg.AdminToken)
	envStr("DATA_DIR", &cfg.DataDir)
	envStr("LOG_LEVEL", &cfg.LogLevel)
	envStr("MONITOR_LISTEN_ADDR", &cfg.MonitorListenAddr)

	return cfg
}

func envStr(key string, dst *string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

func envInt64(key string, dst *int64) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			*dst = n
		}
	}
}
