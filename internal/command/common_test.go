package command

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func TestHandleStickerInfoEscapesTelegramFields(t *testing.T) {
	var sentText string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getStickerSet"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"name":"set<&","title":"title <b>&","sticker_type":"unknown<type","stickers":[]}}`))
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parse sendMessage form: %v", err)
			}
			sentText = r.Form.Get("text")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":0,"chat":{"id":1,"type":"private"}}}`))
		default:
			t.Fatalf("unexpected Telegram method %q", r.URL.Path)
		}
	}))
	defer server.Close()

	b, err := bot.New("test-token", bot.WithServerURL(server.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatalf("new test bot: %v", err)
	}
	h := &Handler{}
	h.HandleStickerInfo(context.Background(), b, 1, &models.Sticker{SetName: "set"})

	for _, raw := range []string{"title <b>&", "set<&", "unknown<type"} {
		if strings.Contains(sentText, raw) {
			t.Fatalf("sent HTML includes unescaped Telegram value %q: %s", raw, sentText)
		}
	}
	for _, escaped := range []string{"title &lt;b&gt;&amp;", "set&lt;&amp;", "unknown&lt;type"} {
		if !strings.Contains(sentText, escaped) {
			t.Fatalf("sent HTML is missing escaped Telegram value %q: %s", escaped, sentText)
		}
	}
}

func TestSendDocumentReportsUploadFailure(t *testing.T) {
	const wantNotification = "Failed to send the file. Please try again. /start\n文件发送失败，请重试。"
	var methods []string
	var notification url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getFile") {
			methods = append(methods, "getFile")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"file-id","file_unique_id":"unique-id","file_path":"stickers/sticker.png"}}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/file/bottest-token/") {
			methods = append(methods, "downloadFile")
			_, _ = w.Write([]byte("not a PNG"))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/sendDocument") {
			methods = append(methods, "sendDocument")
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parse sendDocument multipart form: %v", err)
			}
			_, _ = w.Write([]byte(`{"ok":false,"error_code":500,"description":"upload failed"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			methods = append(methods, "sendMessage")
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parse sendMessage form: %v", err)
			}
			notification = r.Form
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":0,"chat":{"id":1,"type":"private"}}}`))
			return
		}
		t.Fatalf("unexpected Telegram method %q", r.URL.Path)
	}))
	defer server.Close()

	b, err := bot.New("test-token", bot.WithServerURL(server.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatalf("new test bot: %v", err)
	}
	previousClient := httpClient
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	httpClient = &http.Client{Transport: rewriteTransport{target: serverURL, base: http.DefaultTransport}}
	t.Cleanup(func() { httpClient = previousClient })
	h := &Handler{BotToken: "test-token"}

	h.downloadSingleSticker(context.Background(), b, 1, &models.Sticker{FileID: "file-id", SetName: "set"}, t.TempDir())

	if got, want := strings.Join(methods, ","), "getFile,downloadFile,sendDocument,sendMessage"; got != want {
		t.Fatalf("Telegram calls = %q, want %q", got, want)
	}
	if notification.Get("text") != wantNotification {
		t.Fatalf("upload failure notification = %q", notification.Get("text"))
	}
}

type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	clone.Host = t.target.Host
	return t.base.RoundTrip(clone)
}
