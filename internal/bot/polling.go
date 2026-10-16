package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-telegram/bot/models"
	"github.com/xjoker/x_sticker_bot/internal/monitor"
)

const (
	pollTimeout    = time.Minute
	pollStaleAfter = 2 * (pollTimeout + 5*time.Second)
)

type pollingHTTPClient struct {
	client httpClient
	health *monitor.PollingHealth
}

type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}

func newPollingHTTPClient(client httpClient, health *monitor.PollingHealth) *pollingHTTPClient {
	return &pollingHTTPClient{client: client, health: health}
}

func (c *pollingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.client.Do(req)
	if err != nil || !strings.HasSuffix(req.URL.Path, "/getUpdates") || resp == nil || resp.Body == nil {
		return resp, err
	}
	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read getUpdates response: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close getUpdates response: %w", closeErr)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	valid, malformed := validUpdatesResponse(body)
	if malformed {
		return nil, errors.New("invalid getUpdates response")
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices && valid {
		c.health.RecordSuccess(time.Now())
	}
	return resp, nil
}

func validUpdatesResponse(body []byte) (valid bool, malformed bool) {
	var response struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return false, true
	}
	if !response.OK {
		return false, false
	}
	if len(response.Result) == 0 || response.Result[0] != '[' {
		return false, true
	}
	var updates []*models.Update
	if err := json.Unmarshal(response.Result, &updates); err != nil {
		return false, true
	}
	for _, update := range updates {
		if update == nil {
			return false, true
		}
	}
	return true, false
}

func logTelegramError(token string, err error) {
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	message := strings.ReplaceAll(err.Error(), token, "***")
	if strings.Contains(message, "error decode response body") {
		message = "invalid Telegram API response"
	}
	slog.Error("telegram API request failed", "error", message)
}
