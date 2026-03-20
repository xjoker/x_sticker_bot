package command

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/xjoker/x_sticker_bot/internal/message"
	"github.com/xjoker/x_sticker_bot/internal/sticker"
	"github.com/xjoker/x_sticker_bot/internal/storage"
)

// tryRunTask acquires a task slot, runs fn, and releases the slot.
// Returns false if the task could not be acquired (already sent user a message).
// Note: fn runs synchronously; go-telegram/bot dispatches each update in its own goroutine.
func (h *Handler) tryRunTask(ctx context.Context, b *bot.Bot, chatID int64, uid int64, fn func()) bool {
	// Per-user: only one task at a time
	if _, loaded := h.userTasks.LoadOrStore(uid, true); loaded {
		h.sendText(ctx, b, chatID, message.UserBusy())
		return false
	}

	// Global capacity check
	select {
	case h.GlobalSem <- struct{}{}:
		// acquired
	default:
		h.userTasks.Delete(uid)
		h.sendText(ctx, b, chatID, message.ServerBusy())
		return false
	}

	defer func() {
		h.userTasks.Delete(uid)
		<-h.GlobalSem
	}()

	fn()
	return true
}

// recordStat records an anonymized usage event if stats is available.
func (h *Handler) recordStat(uid int64, event string) {
	if h.Stats != nil {
		h.Stats.RecordEvent(uid, event)
	}
}

// HandleDownload downloads a single sticker or an entire sticker set.
// When s is not nil, download that single sticker; otherwise download the whole set by setID.
func (h *Handler) HandleDownload(ctx context.Context, b *bot.Bot, chatID int64, s *models.Sticker, setID string) {
	var id string
	if s != nil {
		if s.SetName != "" {
			id = s.SetName
		} else {
			id = "sticker_" + sticker.SecHex(4)
		}
	} else {
		id = setID
	}

	workDir, err := h.TempMgr.CreateUserDir(0)
	if err != nil {
		slog.Error("failed to create work dir for download", "error", err)
		h.sendText(ctx, b, chatID, message.FatalError(err))
		return
	}
	defer func() {
		if cleanErr := h.TempMgr.CleanUserDir(workDir); cleanErr != nil {
			slog.Warn("failed to clean download work dir", "error", cleanErr)
		}
	}()

	downloadDir := filepath.Join(workDir, id)
	if err := os.MkdirAll(downloadDir, 0750); err != nil {
		slog.Error("failed to create download subdir", "error", err)
		h.sendText(ctx, b, chatID, message.FatalError(err))
		return
	}

	// Single sticker download
	if s != nil {
		h.downloadSingleSticker(ctx, b, chatID, s, downloadDir)
		return
	}

	// Whole sticker set download
	h.downloadStickerSet(ctx, b, chatID, setID, downloadDir)
}

// downloadSingleSticker downloads and sends one sticker.
func (h *Handler) downloadSingleSticker(ctx context.Context, b *bot.Bot, chatID int64, s *models.Sticker, workDir string) {
	namePrefix := s.SetName
	if namePrefix == "" {
		namePrefix = "sticker"
	}
	filePath, err := h.downloadTelegramFile(ctx, b, s.FileID, workDir, namePrefix+"_"+sticker.SecHex(2))
	if err != nil {
		slog.Warn("failed to download single sticker", "error", err)
		h.sendText(ctx, b, chatID, message.FatalError(err))
		return
	}

	// Convert to user-friendly format (original size, no scaling)
	if s.IsVideo || s.IsAnimated {
		gifPath, err := sticker.ToGif(filePath)
		if err != nil {
			slog.Warn("failed to convert sticker to gif", "error", err)
			h.sendText(ctx, b, chatID, message.FatalError(err))
			return
		}
		h.sendDocument(ctx, b, chatID, gifPath, filepath.Base(gifPath))
	} else {
		pngPath, err := sticker.ToPng(filePath)
		if err != nil {
			slog.Warn("failed to convert sticker to png, sending original", "error", err)
			pngPath = filePath
		}
		h.sendDocument(ctx, b, chatID, pngPath, filepath.Base(pngPath))
	}
}

// downloadStickerSet downloads an entire sticker set and sends as ZIP file(s).
func (h *Handler) downloadStickerSet(ctx context.Context, b *bot.Bot, chatID int64, setID string, workDir string) {
	ss, err := b.GetStickerSet(ctx, &bot.GetStickerSetParams{Name: setID})
	if err != nil {
		slog.Warn("failed to get sticker set for download", "id", setID, "error", err)
		h.sendText(ctx, b, chatID, "Cannot find this sticker set. /start")
		return
	}

	total := len(ss.Stickers)

	// Send initial progress message
	progressMsgID := h.sendProgress(ctx, b, chatID, message.DownloadProgress(0, total))

	var mu sync.Mutex
	var originalFiles []string
	var convertedFiles []string
	var completed atomic.Int32
	var lastProgressUpdate atomic.Int64 // unix millis of last edit

	// throttledProgress updates progress message at most once per 2 seconds.
	throttledProgress := func() {
		done := int(completed.Load())
		now := time.Now().UnixMilli()
		last := lastProgressUpdate.Load()
		if done == total || now-last >= 2000 {
			if lastProgressUpdate.CompareAndSwap(last, now) {
				h.editText(ctx, b, chatID, progressMsgID, message.DownloadProgress(done, total))
			}
		}
	}

	// Download concurrently with limited parallelism
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup

	for i, s := range ss.Stickers {
		wg.Add(1)
		go func(idx int, st models.Sticker) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			name := fmt.Sprintf("%s_%d", setID, idx+1)
			fp, err := h.downloadTelegramFile(ctx, b, st.FileID, workDir, name)
			if err != nil {
				slog.Warn("failed to download sticker from set", "index", idx, "error", err)
				completed.Add(1)
				throttledProgress()
				return
			}

			// Convert (original size, no scaling)
			var cf string
			if st.IsVideo || st.IsAnimated {
				cf, err = sticker.ToGif(fp)
			} else {
				cf, err = sticker.ToPng(fp)
			}
			if err != nil {
				slog.Warn("failed to convert sticker from set", "index", idx, "error", err)
				cf = fp // use original on failure
			}

			mu.Lock()
			originalFiles = append(originalFiles, fp)
			convertedFiles = append(convertedFiles, cf)
			mu.Unlock()

			completed.Add(1)
			throttledProgress()
		}(i, s)
	}
	wg.Wait()

	// Delete progress message when done
	h.deleteMessage(ctx, b, chatID, progressMsgID)

	if len(originalFiles) == 0 {
		h.sendText(ctx, b, chatID, message.FatalError(fmt.Errorf("no files downloaded")))
		return
	}

	// Partial success: notify user if some stickers failed
	if len(originalFiles) < total {
		h.sendText(ctx, b, chatID, message.PartialDownload(len(originalFiles), total))
	}

	// Compress and send
	var zipPaths []string

	origZip := filepath.Join(workDir, setID+"_original.zip")
	convZip := filepath.Join(workDir, setID+"_converted.zip")

	origZips, err := sticker.FCompressVol(origZip, originalFiles)
	if err != nil {
		slog.Warn("failed to compress original files", "error", err)
	} else {
		zipPaths = append(zipPaths, origZips...)
	}

	convZips, err := sticker.FCompressVol(convZip, convertedFiles)
	if err != nil {
		slog.Warn("failed to compress converted files", "error", err)
	} else {
		zipPaths = append(zipPaths, convZips...)
	}

	for _, zp := range zipPaths {
		h.sendDocument(ctx, b, chatID, zp, filepath.Base(zp))
	}
}

// HandleMessage handles all non-command messages (stickers, links, callbacks).
func (h *Handler) HandleMessage(ctx context.Context, b *bot.Bot, update *models.Update) {
	chatID := h.chatIDFromUpdate(update)
	uid := h.uidFromUpdate(update)

	// Handle callback queries (only "download whole set" button from previous sticker download).
	if update.CallbackQuery != nil {
		h.answerCallback(ctx, b, update.CallbackQuery.ID)

		botMsg := update.CallbackQuery.Message.Message
		if botMsg == nil {
			h.sendText(ctx, b, chatID, message.SendStickerHint())
			return
		}
		originMsg := botMsg.ReplyToMessage
		if originMsg == nil {
			h.sendText(ctx, b, chatID, message.SendStickerHint())
			return
		}

		switch update.CallbackQuery.Data {
		case message.CbDnWhole:
			sid := h.getSIDFromMessage(originMsg)
			if sid != "" {
				h.tryRunTask(ctx, b, chatID, uid, func() {
					h.recordStat(uid, storage.EventDownloadSet)
					h.HandleDownload(ctx, b, chatID, nil, sid)
				})
			} else {
				h.sendText(ctx, b, chatID, message.SendStickerHint())
			}
		case message.CbStickerInfo:
			if originMsg.Sticker != nil {
				h.recordStat(uid, storage.EventInfo)
				h.HandleStickerInfo(ctx, b, chatID, originMsg.Sticker)
			}
		}
		return
	}

	msg := update.Message
	if msg == nil {
		return
	}

	// Sticker → directly download single, offer "download whole set" button
	if msg.Sticker != nil {
		h.tryRunTask(ctx, b, chatID, uid, func() {
			h.recordStat(uid, storage.EventDownloadSingle)
			progressID := h.sendProgress(ctx, b, chatID, message.ProcessStarted())
			h.HandleDownload(ctx, b, chatID, msg.Sticker, "")
			h.deleteMessage(ctx, b, chatID, progressID)
			if msg.Sticker.SetName != "" {
				text, kb := message.OfferWholeSetDownload()
				h.sendWithKeyboardReply(ctx, b, chatID, msg.ID, text, kb)
			}
		})
		return
	}

	// Animation → convert to GIF
	if msg.Animation != nil {
		h.tryRunTask(ctx, b, chatID, uid, func() {
			h.handleAnimationDownload(ctx, b, chatID, msg)
		})
		return
	}

	// Photo or document
	if msg.Photo != nil || msg.Document != nil {
		h.sendText(ctx, b, chatID, message.SendStickerHint())
		return
	}

	// Text → check for t.me sticker link → directly download whole set
	if msg.Text != "" {
		link, found := findTGLink(msg.Text)
		if found {
			sn := path.Base(link)
			h.tryRunTask(ctx, b, chatID, uid, func() {
				h.recordStat(uid, storage.EventDownloadSet)
				h.HandleDownload(ctx, b, chatID, nil, sn)
			})
			return
		}
	}

	h.sendText(ctx, b, chatID, message.SendStickerHint())
}

// handleAnimationDownload converts an animation (MP4) to GIF and sends it.
func (h *Handler) handleAnimationDownload(ctx context.Context, b *bot.Bot, chatID int64, msg *models.Message) {
	progressID := h.sendProgress(ctx, b, chatID, message.ProcessStarted())

	workDir, err := h.TempMgr.CreateUserDir(0)
	if err != nil {
		slog.Error("failed to create work dir for animation", "error", err)
		h.sendText(ctx, b, chatID, message.FatalError(err))
		return
	}
	defer func() {
		if cleanErr := h.TempMgr.CleanUserDir(workDir); cleanErr != nil {
			slog.Warn("failed to clean animation work dir", "error", cleanErr)
		}
	}()

	fp, err := h.downloadTelegramFile(ctx, b, msg.Animation.FileID, workDir, "animation_MP4")
	if err != nil {
		slog.Warn("failed to download animation", "error", err)
		h.sendText(ctx, b, chatID, message.FatalError(err))
		return
	}

	gifPath, err := sticker.ToGif(fp)
	if err != nil {
		slog.Warn("failed to convert animation to gif", "error", err)
		h.sendText(ctx, b, chatID, message.FatalError(err))
		return
	}

	zipPath := filepath.Join(workDir, sticker.SecHex(4)+".zip")
	if err := sticker.FCompress(zipPath, []string{gifPath}); err != nil {
		slog.Warn("failed to compress gif", "error", err)
		h.sendText(ctx, b, chatID, message.FatalError(err))
		return
	}

	h.sendDocument(ctx, b, chatID, zipPath, filepath.Base(zipPath))
	h.deleteMessage(ctx, b, chatID, progressID)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const (
	maxDownloadBytes = 100 * 1024 * 1024 // 100 MB
	maxRetries       = 3
)

var httpClient = &http.Client{Timeout: 120 * time.Second}

// editText edits an existing message's text. Silently ignores errors.
func (h *Handler) editText(ctx context.Context, b *bot.Bot, chatID int64, msgID int, text string) {
	if msgID == 0 {
		return
	}
	_, err := b.EditMessageText(ctx, &bot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: msgID,
		Text:      text,
		ParseMode: models.ParseModeHTML,
	})
	if err != nil {
		slog.Debug("editText failed", "chat_id", chatID, "msg_id", msgID, "error", err)
	}
}

// downloadTelegramFile downloads a file from Telegram by file ID to workDir/{name}.
// Returns the local file path. Retries on HTTP 429 with exponential backoff.
func (h *Handler) downloadTelegramFile(ctx context.Context, b *bot.Bot, fileID, workDir, name string) (string, error) {
	file, err := b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return "", fmt.Errorf("getFile failed: %w", err)
	}

	ext := filepath.Ext(path.Base(file.FilePath))
	localPath := filepath.Join(workDir, name+ext)

	downloadURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", h.BotToken, file.FilePath)

	var resp *http.Response
	for attempt := range maxRetries {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
		if err != nil {
			return "", fmt.Errorf("download request failed")
		}

		resp, err = httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("download failed: connection error")
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()

			waitDur := time.Duration(1<<uint(attempt)) * time.Second // 1s, 2s, 4s
			if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
				if sec, parseErr := strconv.Atoi(retryAfter); parseErr == nil && sec > 0 {
					waitDur = time.Duration(sec) * time.Second
				}
			}

			slog.Warn("download rate limited (429), retrying", "attempt", attempt+1, "wait", waitDur)

			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(waitDur):
				continue
			}
		}

		break
	}

	if resp == nil {
		return "", fmt.Errorf("download failed: no response after retries")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.Create(localPath)
	if err != nil {
		return "", fmt.Errorf("create file failed: %w", err)
	}
	defer f.Close()

	limited := io.LimitReader(resp.Body, maxDownloadBytes+1)
	n, err := io.Copy(f, limited)
	if err != nil {
		return "", fmt.Errorf("write file failed: %w", err)
	}
	if n > maxDownloadBytes {
		os.Remove(localPath)
		return "", fmt.Errorf("file exceeds maximum allowed size (100MB)")
	}

	return localPath, nil
}

// sendDocument sends a file as a document to the chat.
func (h *Handler) sendDocument(ctx context.Context, b *bot.Bot, chatID int64, filePath, fileName string) {
	f, err := os.Open(filePath)
	if err != nil {
		slog.Warn("sendDocument: failed to open file", "path", filePath, "error", err)
		return
	}
	defer f.Close()

	_, err = b.SendDocument(ctx, &bot.SendDocumentParams{
		ChatID:   chatID,
		Document: &models.InputFileUpload{Filename: fileName, Data: f},
	})
	if err != nil {
		slog.Debug("sendDocument failed", "file", fileName, "error", err)
	}
}

// getSIDFromMessage extracts a sticker set ID from a message (from sticker or text link).
func (h *Handler) getSIDFromMessage(msg *models.Message) string {
	if msg.Sticker != nil {
		return msg.Sticker.SetName
	}
	link, found := findTGLink(msg.Text)
	if found {
		return path.Base(link)
	}
	return ""
}

// findTGLink extracts a t.me sticker link from text.
func findTGLink(text string) (link string, found bool) {
	if !strings.Contains(text, "t.me") && !strings.Contains(text, "telegram.me") {
		return "", false
	}
	for _, word := range strings.Fields(text) {
		if !strings.Contains(word, "t.me") && !strings.Contains(word, "telegram.me") {
			continue
		}
		u, err := url.Parse(word)
		if err != nil {
			continue
		}
		if u.Host == "t.me" || u.Host == "telegram.me" {
			return word, true
		}
	}
	return "", false
}
