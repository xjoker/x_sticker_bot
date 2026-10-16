package message

import (
	"errors"
	"strings"
	"testing"
)

func TestFatalErrorEscapesHTML(t *testing.T) {
	text := FatalError(errors.New("failed: <unsafe & value>"))
	if strings.Contains(text, "<unsafe & value>") {
		t.Fatalf("FatalError left external error text unescaped: %q", text)
	}
	if !strings.Contains(text, "&lt;unsafe &amp; value&gt;") {
		t.Fatalf("FatalError did not preserve escaped error text: %q", text)
	}
}
