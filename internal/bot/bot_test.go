package bot

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/xjoker/x_sticker_bot/internal/monitor"
	"github.com/xjoker/x_sticker_bot/internal/ratelimit"
)

type countingHandler struct{ info int }

func (h *countingHandler) CmdStart(context.Context, *tgbot.Bot, *models.Update)      {}
func (h *countingHandler) CmdAbout(context.Context, *tgbot.Bot, *models.Update)      {}
func (h *countingHandler) CmdInfo(context.Context, *tgbot.Bot, *models.Update)       { h.info++ }
func (h *countingHandler) CmdGetFID(context.Context, *tgbot.Bot, *models.Update)     {}
func (h *countingHandler) HandleMessage(context.Context, *tgbot.Bot, *models.Update) {}

func TestRegisterCommandHandlersRateLimitsInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"ok":true,"result":true}`)) }))
	defer server.Close()
	b, err := tgbot.New("1:test", tgbot.WithSkipGetMe(), tgbot.WithNotAsyncHandlers(), tgbot.WithServerURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	handler := &countingHandler{}
	app := &App{Bot: b, Limiter: ratelimit.New(100, 200), Metrics: monitor.NewMetrics(), cmdHandler: handler}
	app.registerCommandHandlers(b)
	update := &models.Update{Message: &models.Message{
		Text:     "/info",
		From:     &models.User{ID: 7},
		Chat:     models.Chat{ID: 7},
		Entities: []models.MessageEntity{{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: 5}},
	}}
	for range 3 {
		b.ProcessUpdate(context.Background(), update)
	}
	if handler.info != 2 {
		t.Fatalf("/info calls = %d, want 2", handler.info)
	}
}

func TestLogTelegramErrorUsesErrorLevelAndRedactsResponse(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	logTelegramError("secret-token", errors.New("error decode response body for secret-token: {\"user\":\"private\"}"))
	logged := output.String()
	if !bytes.Contains([]byte(logged), []byte(`"level":"ERROR"`)) {
		t.Fatalf("expected ERROR log, got %s", logged)
	}
	if bytes.Contains([]byte(logged), []byte("secret-token")) || bytes.Contains([]byte(logged), []byte("private")) {
		t.Fatalf("sensitive API response leaked: %s", logged)
	}
}
