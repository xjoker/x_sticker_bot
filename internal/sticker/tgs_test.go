package sticker

import (
	"bytes"
	"compress/gzip"
	"image/gif"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTGS(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sticker.tgs")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	z := gzip.NewWriter(f)
	if _, err := z.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestToGifConvertsTGS(t *testing.T) {
	if os.Getenv("STICKER_INTEGRATION") != "1" {
		t.Skip("set STICKER_INTEGRATION=1 with lottie_to_png and ffmpeg installed")
	}
	input := writeTGS(t, `{"v":"5.7.4","fr":30,"ip":0,"op":30,"w":512,"h":512,"nm":"moving-square","ddd":0,"assets":[],"layers":[{"ddd":0,"ind":1,"ty":4,"nm":"square","sr":1,"ks":{"o":{"a":0,"k":100},"r":{"a":0,"k":0},"p":{"a":1,"k":[{"t":0,"s":[128,256,0],"e":[384,256,0],"o":{"x":0.33,"y":0.33},"i":{"x":0.67,"y":0.67}},{"t":30,"s":[384,256,0]}]},"a":{"a":0,"k":[0,0,0]},"s":{"a":0,"k":[100,100,100]}},"shapes":[{"ty":"rc","d":1,"s":{"a":0,"k":[100,100]},"p":{"a":0,"k":[0,0]},"r":{"a":0,"k":0}},{"ty":"fl","c":{"a":0,"k":[1,0,0,1]},"o":{"a":0,"k":100},"r":1}],"ip":0,"op":30,"st":0,"bm":0}]}`)
	output, err := ToGif(input)
	if err != nil {
		t.Fatalf("valid TGS conversion failed: %v", err)
	}
	f, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	animation, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatalf("output is not a GIF: %v", err)
	}
	if len(animation.Image) < 2 {
		t.Fatalf("got %d frames, want animation", len(animation.Image))
	}
	if bytes.Equal(animation.Image[0].Pix, animation.Image[len(animation.Image)-1].Pix) {
		t.Fatal("moving square has identical first and last frames")
	}
	if animation.Config.Width != 256 || animation.Config.Height != 256 {
		t.Fatalf("got %dx%d, want 256x256", animation.Config.Width, animation.Config.Height)
	}
	var duration int
	for _, delay := range animation.Delay {
		duration += delay
	}
	if duration < 90 || duration > 110 {
		t.Errorf("duration=%d centiseconds, want about 100", duration)
	}
	entries, err := os.ReadDir(filepath.Dir(input))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("temporary frames remain: %s", entry.Name())
		}
	}
}

func TestReadTGSRejectsInvalidGzip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.tgs")
	if err := os.WriteFile(path, []byte("not gzip"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTGS(path); err == nil {
		t.Error("invalid gzip must fail")
	}
}

func TestReadTGSRejectsOversizedDecompressedData(t *testing.T) {
	input := writeTGS(t, strings.Repeat(" ", maxTGSJSONSize+1))
	if _, err := readTGS(input); err == nil {
		t.Fatal("oversized decompressed TGS must fail")
	}
}

func TestValidateLottieTiming(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		want bool
	}{
		{"invalid JSON", `{`, false},
		{"zero frame rate", `{"fr":0,"ip":0,"op":30}`, false},
		{"over three seconds", `{"fr":15,"ip":0,"op":46}`, false},
		{"image path asset", `{"fr":15,"ip":0,"op":45,"assets":[{"p":"image.png"}]}`, false},
		{"image URL asset", `{"fr":15,"ip":0,"op":45,"assets":[{"u":"images/"}]}`, false},
		{"embedded image asset", `{"fr":15,"ip":0,"op":45,"assets":[{"e":1}]}`, false},
		{"precomposition asset", `{"fr":15,"ip":0,"op":45,"assets":[{"id":"comp","layers":[]}]}`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLottieTiming([]byte(tt.data))
			if (err == nil) != tt.want {
				t.Fatalf("validateLottieTiming(%s) error=%v, want valid=%v", tt.data, err, tt.want)
			}
		})
	}
}
