package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Event name constants for usage tracking.
const (
	EventDownloadSingle = "download_single"
	EventDownloadSet    = "download_set"
	EventInfo           = "info"
)

// Stats provides anonymized usage statistics storage.
type Stats struct {
	db *sql.DB
}

// DailyStat represents one day's aggregated statistics.
type DailyStat struct {
	Date            string `json:"date"`
	UniqueUsers     int    `json:"unique_users"`
	SingleDownloads int    `json:"single_downloads"`
	SetDownloads    int    `json:"set_downloads"`
	InfoQueries     int    `json:"info_queries"`
}

// NewStats opens (or creates) a SQLite database for usage statistics.
func NewStats(dbPath string) (*Stats, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("storage: create stats db directory: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)", dbPath)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: open stats db: %w", err)
	}
	conn.SetMaxOpenConns(1)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("storage: ping stats db: %w", err)
	}

	s := &Stats{db: conn}
	if err := s.initSchema(); err != nil {
		conn.Close()
		return nil, err
	}

	slog.Info("stats database opened", "path", dbPath)
	return s, nil
}

func (s *Stats) initSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS daily_events (
			date TEXT NOT NULL,
			user_hash TEXT NOT NULL,
			event TEXT NOT NULL,
			count INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_daily_events_unique
			ON daily_events(date, user_hash, event)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("storage: stats schema: %w", err)
		}
	}
	return nil
}

// RecordEvent records an anonymized usage event.
// The user ID is hashed and truncated to 12 hex chars — enough for unique counting,
// impossible to reverse to the original ID.
func (s *Stats) RecordEvent(uid int64, event string) {
	date := time.Now().UTC().Format("2006-01-02")
	hash := hashUID(uid)

	_, err := s.db.Exec(
		`INSERT INTO daily_events (date, user_hash, event, count)
		 VALUES (?, ?, ?, 1)
		 ON CONFLICT(date, user_hash, event) DO UPDATE SET count = count + 1`,
		date, hash, event,
	)
	if err != nil {
		slog.Warn("stats: record event failed", "error", err)
	}
}

// QueryDaily returns aggregated stats for the last N days.
func (s *Stats) QueryDaily(days int) ([]DailyStat, error) {
	since := time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")

	rows, err := s.db.Query(`
		SELECT date,
			COUNT(DISTINCT user_hash) AS unique_users,
			COALESCE(SUM(CASE WHEN event = 'download_single' THEN count END), 0) AS single_dl,
			COALESCE(SUM(CASE WHEN event = 'download_set' THEN count END), 0) AS set_dl,
			COALESCE(SUM(CASE WHEN event = 'info' THEN count END), 0) AS info_q
		FROM daily_events
		WHERE date >= ?
		GROUP BY date
		ORDER BY date DESC`, since)
	if err != nil {
		return nil, fmt.Errorf("storage: query daily stats: %w", err)
	}
	defer rows.Close()

	var result []DailyStat
	for rows.Next() {
		var d DailyStat
		if err := rows.Scan(&d.Date, &d.UniqueUsers, &d.SingleDownloads, &d.SetDownloads, &d.InfoQueries); err != nil {
			return nil, fmt.Errorf("storage: scan daily stat: %w", err)
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

// TotalStats returns all-time totals.
func (s *Stats) TotalStats() (uniqueUsers int, totalDownloads int, err error) {
	err = s.db.QueryRow(`
		SELECT COUNT(DISTINCT user_hash),
			COALESCE(SUM(CASE WHEN event IN ('download_single', 'download_set') THEN count END), 0)
		FROM daily_events`).Scan(&uniqueUsers, &totalDownloads)
	if err != nil {
		err = fmt.Errorf("storage: total stats: %w", err)
	}
	return
}

// QueryDailyJSON returns daily stats and totals as a single call (satisfies monitor.StatsProvider).
func (s *Stats) QueryDailyJSON(days int) (daily any, totalUsers int, totalDownloads int, err error) {
	d, err := s.QueryDaily(days)
	if err != nil {
		return nil, 0, 0, err
	}
	totalUsers, totalDownloads, err = s.TotalStats()
	if err != nil {
		return d, 0, 0, err
	}
	return d, totalUsers, totalDownloads, nil
}

// Close closes the database.
func (s *Stats) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

var statsSalt string

func init() {
	statsSalt = os.Getenv("STATS_SALT")
	if statsSalt == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
		}
		statsSalt = hex.EncodeToString(b)
		slog.Warn("STATS_SALT not set, generated random salt (stats won't persist across restarts)")
	}
}

// hashUID produces a truncated SHA256 hash of the user ID.
// 12 hex chars = 6 bytes = enough for unique counting, not reversible.
func hashUID(uid int64) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s_%d", statsSalt, uid)))
	return hex.EncodeToString(h[:6])
}
