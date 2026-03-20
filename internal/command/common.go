package command

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	appbot "github.com/xjoker/x_sticker_bot/internal/bot"
	"github.com/xjoker/x_sticker_bot/internal/message"
	"github.com/xjoker/x_sticker_bot/internal/monitor"
	"github.com/xjoker/x_sticker_bot/internal/storage"
)

const MaxGlobalTasks = 20

// Handler holds all dependencies for command handlers.
type Handler struct {
	Bot      *bot.Bot
	TempMgr  *storage.TempManager
	Stats    *storage.Stats
	Metrics  *monitor.Metrics
	BotName  string
	BotToken string // needed for file download URL construction
	AdminUID int64

	GlobalSem chan struct{} // global concurrency limiter, init in factory
	userTasks sync.Map     // map[int64]bool — tracks active user tasks
}

// ---------------------------------------------------------------------------
// Helper methods
// ---------------------------------------------------------------------------

// sendText sends a plain HTML message to the given chat.
func (h *Handler) sendText(ctx context.Context, b *bot.Bot, chatID int64, text string) {
	h.sendProgress(ctx, b, chatID, text)
}

// sendProgress sends a message and returns its message ID for later deletion/editing.
func (h *Handler) sendProgress(ctx context.Context, b *bot.Bot, chatID int64, text string) int {
	sent, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:    chatID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
	})
	if err != nil {
		slog.Debug("sendMessage failed", "error", err)
		return 0
	}
	return sent.ID
}

// deleteMessage deletes a previously sent message. Silently ignores errors.
func (h *Handler) deleteMessage(ctx context.Context, b *bot.Bot, chatID int64, msgID int) {
	if msgID == 0 {
		return
	}
	_, err := b.DeleteMessage(ctx, &bot.DeleteMessageParams{
		ChatID:    chatID,
		MessageID: msgID,
	})
	if err != nil {
		slog.Debug("deleteMessage failed", "chat_id", chatID, "msg_id", msgID, "error", err)
	}
}

// sendWithKeyboardReply sends an HTML message with inline keyboard, optionally replying to a message.
func (h *Handler) sendWithKeyboardReply(ctx context.Context, b *bot.Bot, chatID int64, replyToMsgID int, text string, kb *models.InlineKeyboardMarkup) {
	params := &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	}
	if replyToMsgID != 0 {
		params.ReplyParameters = &models.ReplyParameters{
			MessageID: replyToMsgID,
		}
	}
	_, err := b.SendMessage(ctx, params)
	if err != nil {
		slog.Debug("sendWithKeyboard failed", "error", err)
	}
}

// uidFromUpdate extracts user ID from either message or callback query.
func (h *Handler) uidFromUpdate(update *models.Update) int64 {
	if update.CallbackQuery != nil {
		return update.CallbackQuery.From.ID
	}
	if update.Message != nil {
		return update.Message.From.ID
	}
	return 0
}

// chatIDFromUpdate extracts chat ID from either message or callback query.
func (h *Handler) chatIDFromUpdate(update *models.Update) int64 {
	if update.CallbackQuery != nil && update.CallbackQuery.Message.Message != nil {
		return update.CallbackQuery.Message.Message.Chat.ID
	}
	if update.Message != nil {
		return update.Message.Chat.ID
	}
	return 0
}

// answerCallback responds to a callback query to dismiss the loading indicator.
func (h *Handler) answerCallback(ctx context.Context, b *bot.Bot, callbackID string) {
	_, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: callbackID,
	})
	if err != nil {
		slog.Debug("answerCallback failed", "callback_id", callbackID, "error", err)
	}
}

// ---------------------------------------------------------------------------
// Command handlers
// ---------------------------------------------------------------------------

// CmdStart handles /start and /help.
func (h *Handler) CmdStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	h.sendText(ctx, b, update.Message.Chat.ID, message.StartMessage())
}

// CmdAbout sends version and privacy info.
func (h *Handler) CmdAbout(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	h.sendText(ctx, b, update.Message.Chat.ID, message.AboutMessage(appbot.BotVersion))
}

// CmdGetFID replies with the file ID of any media in the message. Admin only.
func (h *Handler) CmdGetFID(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	uid := update.Message.From.ID
	chatID := update.Message.Chat.ID

	if uid != h.AdminUID {
		return
	}

	msg := update.Message
	var fileID string

	switch {
	case msg.Sticker != nil:
		fileID = msg.Sticker.FileID
	case msg.Photo != nil && len(msg.Photo) > 0:
		fileID = msg.Photo[len(msg.Photo)-1].FileID
	case msg.Animation != nil:
		fileID = msg.Animation.FileID
	case msg.Video != nil:
		fileID = msg.Video.FileID
	case msg.Document != nil:
		fileID = msg.Document.FileID
	case msg.Audio != nil:
		fileID = msg.Audio.FileID
	case msg.Voice != nil:
		fileID = msg.Voice.FileID
	case msg.VideoNote != nil:
		fileID = msg.VideoNote.FileID
	default:
		h.sendText(ctx, b, chatID, "No media found.")
		return
	}

	h.sendText(ctx, b, chatID, "<code>"+fileID+"</code>")
}

// CmdInfo handles /info — prompts user to send a sticker.
func (h *Handler) CmdInfo(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	h.sendText(ctx, b, update.Message.Chat.ID,
		"Send a sticker to view its set info.\n发送一个贴纸来查看贴纸包详情。")
}

// HandleStickerInfo looks up a sticker set and sends its details.
func (h *Handler) HandleStickerInfo(ctx context.Context, b *bot.Bot, chatID int64, s *models.Sticker) {
	if s.SetName == "" {
		h.sendText(ctx, b, chatID, "This sticker does not belong to any set.\n该贴纸不属于任何贴纸包。")
		return
	}

	ss, err := b.GetStickerSet(ctx, &bot.GetStickerSetParams{Name: s.SetName})
	if err != nil {
		slog.Warn("failed to get sticker set info", "name", s.SetName, "error", err)
		h.sendText(ctx, b, chatID, "Failed to get sticker set info.\n无法获取贴纸包信息。")
		return
	}

	stickerType := ss.StickerType
	switch stickerType {
	case "regular":
		stickerType = "Regular / 普通贴纸"
	case "mask":
		stickerType = "Mask / 面具贴纸"
	case "custom_emoji":
		stickerType = "Custom Emoji / 自定义 Emoji"
	}

	var staticCount, videoCount, animatedCount int
	for _, st := range ss.Stickers {
		switch {
		case st.IsVideo:
			videoCount++
		case st.IsAnimated:
			animatedCount++
		default:
			staticCount++
		}
	}

	formatInfo := ""
	if staticCount > 0 {
		formatInfo += fmt.Sprintf("  Static / 静态: %d\n", staticCount)
	}
	if videoCount > 0 {
		formatInfo += fmt.Sprintf("  Video / 视频: %d\n", videoCount)
	}
	if animatedCount > 0 {
		formatInfo += fmt.Sprintf("  Animated / 动画: %d\n", animatedCount)
	}

	text := fmt.Sprintf(`<b>Sticker Set Info / 贴纸包详情</b>

<b>Title / 标题:</b> %s
<b>ID:</b> <code>%s</code>
<b>Type / 类型:</b> %s
<b>Count / 数量:</b> %d
%s<b>Link / 链接:</b> https://t.me/addstickers/%s`,
		ss.Title, ss.Name, stickerType, len(ss.Stickers), formatInfo, ss.Name)

	h.sendText(ctx, b, chatID, text)
}

