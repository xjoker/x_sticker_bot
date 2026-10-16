package bot

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xjoker/x_sticker_bot/internal/monitor"
)

func TestPollingHTTPClientOnlyRecordsValidGetUpdatesResponses(t *testing.T) {
	responses := map[string]struct {
		status int
		body   string
	}{
		"ok":        {http.StatusOK, `{"ok":true,"result":[]}`},
		"apierror":  {http.StatusOK, `{"ok":false,"error_code":502}`},
		"invalid":   {http.StatusOK, `{`},
		"null":      {http.StatusOK, `{"ok":true,"result":null}`},
		"nullitem":  {http.StatusOK, `{"ok":true,"result":[null]}`},
		"servererr": {http.StatusBadGateway, `{"ok":true,"result":[]}`},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
		resp := responses[key]
		w.WriteHeader(resp.status)
		_, _ = io.WriteString(w, resp.body)
	}))
	defer server.Close()

	for key, want := range map[string]struct{ healthy, wantErr bool }{
		"ok": {true, false}, "apierror": {false, false}, "invalid": {false, true},
		"null": {false, true}, "nullitem": {false, true}, "servererr": {false, false},
	} {
		health := monitor.NewPollingHealth(130 * time.Second)
		client := newPollingHTTPClient(server.Client(), health)
		req, err := http.NewRequest(http.MethodPost, server.URL+"/"+key+"/getUpdates", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if (err != nil) != want.wantErr {
			t.Fatalf("%s: error=%v, want error=%v", key, err, want.wantErr)
		}
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if string(body) != responses[key].body {
			t.Errorf("%s: response body changed", key)
		}
		if got := health.Status(time.Now()); (got == monitor.PollHealthy) != want.healthy {
			t.Errorf("%s: healthy=%v, status=%s", key, want.healthy, got)
		}
	}

	// A successful call to any other Telegram method must not hide polling failure.
	health := monitor.NewPollingHealth(130 * time.Second)
	health.RecordSuccess(time.Now().Add(-131 * time.Second))
	client := newPollingHTTPClient(server.Client(), health)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/ok/sendMessage", bytes.NewReader(nil))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := health.Status(time.Now()); got == monitor.PollHealthy {
		t.Fatalf("non-polling response incorrectly recovered health")
	}
}

type httpClientFunc func(*http.Request) (*http.Response, error)

func (f httpClientFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestPollingHTTPClientDoesNotTreatTransportFailureAsSuccess(t *testing.T) {
	health := monitor.NewPollingHealth(130 * time.Second)
	client := newPollingHTTPClient(httpClientFunc(func(*http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	}), health)
	req, _ := http.NewRequest(http.MethodPost, "https://example.test/bot/token/getUpdates", nil)
	if _, err := client.Do(req); err == nil {
		t.Fatal("expected transport error")
	}
	if got := health.Status(time.Now()); got == monitor.PollHealthy {
		t.Fatalf("transport failure marked polling healthy")
	}
}

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failingReadCloser) Close() error             { return nil }

func TestPollingHTTPClientDoesNotTreatReadFailureAsSuccess(t *testing.T) {
	health := monitor.NewPollingHealth(130 * time.Second)
	client := newPollingHTTPClient(httpClientFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: failingReadCloser{}}, nil
	}), health)
	req, _ := http.NewRequest(http.MethodPost, "https://example.test/bot/token/getUpdates", nil)
	if _, err := client.Do(req); err == nil {
		t.Fatal("expected read error")
	}
	if got := health.Status(time.Now()); got == monitor.PollHealthy {
		t.Fatalf("read failure marked polling healthy")
	}
}
