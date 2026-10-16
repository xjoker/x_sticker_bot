package command

import "testing"

func TestFindTGLinkAcceptsOnlyStickerSetLinks(t *testing.T) {
	for _, tt := range []struct {
		text string
		want bool
	}{
		{"https://t.me/addstickers/Cats_123", true},
		{"download https://telegram.me/addstickers/Cats_123 please", true},
		{"http://t.me/addstickers/Cats_123", true},
		{"https://t.me/..", false},
		{"https://t.me/addstickers/%2e%2e", false},
		{"https://t.me/addstickers/../Cats", false},
		{"https://t.me/addstickers/Cats/extra", false},
		{"https://t.me/addstickers/", false},
		{"https://t.me/username", false},
		{"https://t.me/addemoji/Cats", false},
		{"ftp://t.me/addstickers/Cats", false},
		{"https://evil.example/addstickers/Cats", false},
		{"https://t.me/addstickers/Cats?x=1", false},
		{"https://t.me/addstickers/Cats?", false},
		{"https://t.me/addstickers/Cats#section", false},
		{"https://t.me/addstickers/Cats#", false},
		{"https://user@t.me/addstickers/Cats", false},
	} {
		t.Run(tt.text, func(t *testing.T) {
			_, got := findTGLink(tt.text)
			if got != tt.want {
				t.Fatalf("findTGLink(%q) accepted=%v, want %v", tt.text, got, tt.want)
			}
		})
	}
}
