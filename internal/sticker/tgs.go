package sticker

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	maxTGSJSONSize = 5 * MiB
	tgsTimeout     = 120 * time.Second
)

type lottieTiming struct {
	FrameRate float64                      `json:"fr"`
	InPoint   float64                      `json:"ip"`
	OutPoint  float64                      `json:"op"`
	Assets    []map[string]json.RawMessage `json:"assets"`
}

func tgsToGif(input string) (string, error) {
	jsonData, err := readTGS(input)
	if err != nil {
		return "", err
	}
	if err := validateLottieTiming(jsonData); err != nil {
		return "", err
	}

	framesDir, err := os.MkdirTemp(filepath.Dir(input), ".tgs-frames-")
	if err != nil {
		return "", fmt.Errorf("create TGS frames directory: %w", err)
	}
	defer os.RemoveAll(framesDir)

	jsonPath := filepath.Join(framesDir, "animation.json")
	if err := os.WriteFile(jsonPath, jsonData, 0600); err != nil {
		return "", fmt.Errorf("write TGS animation JSON: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), tgsTimeout)
	defer cancel()
	if out, err := exec.CommandContext(ctx, lottieToPNGBin, jsonPath, "--output", framesDir, "--width", "256", "--height", "256", "--fps", "15", "--threads", "1").CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("render TGS: %w", ctx.Err())
		}
		return "", fmt.Errorf("render TGS: %w: %s", err, out)
	}

	return pngFramesToGif(ctx, filepath.Join(framesDir, "%03d.png"), input+".gif")
}

func readTGS(input string) ([]byte, error) {
	f, err := os.Open(input)
	if err != nil {
		return nil, fmt.Errorf("open TGS: %w", err)
	}
	defer f.Close()

	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("read TGS gzip: %w", err)
	}
	defer zr.Close()

	data, err := io.ReadAll(io.LimitReader(zr, maxTGSJSONSize+1))
	if err != nil {
		return nil, fmt.Errorf("decompress TGS: %w", err)
	}
	if len(data) > maxTGSJSONSize {
		return nil, fmt.Errorf("decompressed TGS exceeds %d bytes", maxTGSJSONSize)
	}
	return data, nil
}

func validateLottieTiming(data []byte) error {
	var timing lottieTiming
	if err := json.Unmarshal(data, &timing); err != nil {
		return fmt.Errorf("parse TGS JSON: %w", err)
	}
	if !finite(timing.FrameRate) || !finite(timing.InPoint) || !finite(timing.OutPoint) || timing.FrameRate <= 0 || timing.OutPoint <= timing.InPoint {
		return fmt.Errorf("invalid TGS frame timing")
	}
	if (timing.OutPoint-timing.InPoint)/timing.FrameRate > 3 {
		return fmt.Errorf("TGS duration exceeds 3 seconds")
	}
	for _, asset := range timing.Assets {
		for _, key := range []string{"p", "u", "e"} {
			if _, ok := asset[key]; ok {
				return fmt.Errorf("TGS image assets are not supported")
			}
		}
	}
	return nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func pngFramesToGif(ctx context.Context, pattern, output string) (string, error) {
	args := []string{"-framerate", "15", "-start_number", "0", "-i", pattern, "-hide_banner",
		"-lavfi", "fps=15,split[a][b];[a]palettegen[p];[b][p]paletteuse=dither=atkinson",
		"-gifflags", "-transdiff", "-gifflags", "-offsetting",
		"-loglevel", "error", "-y", output}
	if out, err := exec.CommandContext(ctx, ffmpegBin, args...).CombinedOutput(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("encode TGS GIF: %w", ctx.Err())
		}
		return "", fmt.Errorf("encode TGS GIF: %w: %s", err, out)
	}
	if _, err := os.Stat(output); err != nil {
		return "", fmt.Errorf("read TGS GIF output: %w", err)
	}
	return output, nil
}
