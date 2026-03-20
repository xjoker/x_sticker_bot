package sticker

import (
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

var ffmpegBin = "ffmpeg"
var bsdtarBin = "bsdtar"
var convertBin = "convert"
var identifyBin = "identify"
var convertArgs []string
var identifyArgs []string

// See: http://en.wikipedia.org/wiki/Binary_prefix
const (
	KB = 1000
	MB = 1000 * KB

	KiB = 1024
	MiB = 1024 * KiB
)

// InitConvert detects the platform and sets ImageMagick binary names accordingly.
// On macOS/Windows, "magick" is used; on Linux, "convert" is used.
// It also calls CheckDeps to verify that required executables are available.
func InitConvert() {
	switch runtime.GOOS {
	case "linux":
		convertBin = "convert"
	default:
		convertBin = "magick"
		identifyBin = "magick"
		convertArgs = []string{"convert"}
		identifyArgs = []string{"identify"}
	}

	missing := CheckDeps()
	if len(missing) > 0 {
		slog.Warn("required executables not found, some features will not work",
			"missing", strings.Join(missing, ", "))
	}
}

// CheckDeps verifies that required external tools (ffmpeg, bsdtar, imagemagick, gifsicle)
// exist in PATH and returns a list of missing binaries.
func CheckDeps() []string {
	var missing []string

	bins := []string{ffmpegBin, bsdtarBin, convertBin, "gifsicle"}
	for _, bin := range bins {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}
	return missing
}

// ToPng converts any image to PNG format (for download/preview).
func ToPng(f string) (string, error) {
	pathOut := f + ".png"
	bin := convertBin
	args := append([]string{}, convertArgs...)
	args = append(args, f+"[0]", pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		slog.Warn("ToPng failed", "error", err, "output", string(out))
		return "", err
	}
	return pathOut, nil
}

// ToWebpStatic converts any image to a static WEBP suitable for Telegram stickers.
// Regular stickers are scaled to 512x512, custom emoji to 100x100.
// If the result exceeds 255KiB, a lossy fallback is attempted.
func ToWebpStatic(f string, isCustomEmoji bool) (string, error) {
	pathOut := f + ".webp"
	bin := convertBin
	args := append([]string{}, convertArgs...)

	if isCustomEmoji {
		args = append(args, "-resize", "100x100", "-gravity", "center", "-extent", "100x100", "-background", "none")
	} else {
		args = append(args, "-resize", "512x512")
	}
	args = append(args, "-filter", "Lanczos", "-define", "webp:lossless=true", f+"[0]", pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		slog.Warn("ToWebpStatic failed", "error", err, "output", string(out))
		return "", err
	}

	st, err := os.Stat(pathOut)
	if err != nil {
		return "", err
	}

	// If lossless WEBP exceeds 255KiB, retry without lossless flag (lossy fallback).
	// 100x100 custom emoji should never exceed this limit.
	if st.Size() > 255*KiB {
		fallbackArgs := append([]string{}, convertArgs...)
		fallbackArgs = append(fallbackArgs, "-resize", "512x512", "-filter", "Lanczos", f+"[0]", pathOut)
		out, err := exec.Command(bin, fallbackArgs...).CombinedOutput()
		if err != nil {
			slog.Warn("ToWebpStatic lossy fallback failed", "error", err, "output", string(out))
			return "", err
		}
	}

	return pathOut, nil
}

// ToWebmVideo converts a video file to VP9 WEBM suitable for Telegram animated stickers.
// It tries multiple bitrate levels to fit within the 255KiB size limit.
func ToWebmVideo(f string, isCustomEmoji bool) (string, error) {
	pathOut := f + ".webm"
	bin := ffmpegBin

	baseArgs := []string{"-hide_banner", "-i", f}
	if isCustomEmoji {
		baseArgs = append(baseArgs, "-vf", "scale=100:100:force_original_aspect_ratio=decrease")
	} else {
		baseArgs = append(baseArgs, "-vf", "scale=512:512:force_original_aspect_ratio=decrease")
	}
	baseArgs = append(baseArgs, "-pix_fmt", "yuva420p", "-c:v", "libvpx-vp9", "-cpu-used", "5")

	// Try progressively lower bitrates to fit under 255KiB.
	bitrateProfiles := [][]string{
		{"-minrate", "50k", "-b:v", "350k", "-maxrate", "450k"},
		{"-minrate", "50k", "-b:v", "200k", "-maxrate", "300k"},
		{"-minrate", "20k", "-b:v", "100k", "-maxrate", "200k"},
		{"-minrate", "10k", "-b:v", "50k", "-maxrate", "100k"},
	}

	for i, profile := range bitrateProfiles {
		args := append(append([]string{}, baseArgs...), profile...)
		args = append(args, "-to", "00:00:03", "-an", "-y", pathOut)

		out, err := exec.Command(bin, args...).CombinedOutput()
		if err != nil {
			slog.Warn("ToWebmVideo encode failed", "attempt", i, "error", err, "output", string(out))

			// FFmpeg does not support animated webp input directly.
			// If we detect this, convert to APNG first then retry.
			if strings.Contains(string(out), "skipping unsupported chunk: ANIM") {
				slog.Warn("ToWebmVideo: animated webp detected, converting to APNG first")
				apngPath, apngErr := toApng(f)
				if apngErr != nil {
					return "", apngErr
				}
				return ToWebmVideo(apngPath, isCustomEmoji)
			}
			return "", err
		}

		stat, err := os.Stat(pathOut)
		if err != nil {
			return "", err
		}
		if stat.Size() <= 255*KiB {
			return pathOut, nil
		}
		slog.Debug("ToWebmVideo output too large, trying lower bitrate",
			"attempt", i, "size", stat.Size())
	}

	slog.Error("ToWebmVideo: unable to compress below 256KiB", "file", pathOut)
	return pathOut, errors.New("unable to compress video below 256KiB")
}

// ToWebmSafe converts a video to WEBM with conservative settings.
// It uses a shorter duration (2.8s) and fixed 30fps, which helps when
// Telegram rejects a video due to overlength or bad FPS.
func ToWebmSafe(f string, isCustomEmoji bool) (string, error) {
	pathOut := f + ".webm"
	bin := ffmpegBin

	args := []string{"-hide_banner", "-i", f}
	if isCustomEmoji {
		args = append(args, "-vf", "scale=100:100:force_original_aspect_ratio=decrease")
	} else {
		args = append(args, "-vf", "scale=512:512:force_original_aspect_ratio=decrease:flags=lanczos")
	}
	args = append(args, "-pix_fmt", "yuva420p",
		"-c:v", "libvpx-vp9", "-cpu-used", "5",
		"-minrate", "50k", "-b:v", "200k", "-maxrate", "300k",
		"-to", "00:00:02.800", "-r", "30", "-an", "-y", pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		slog.Warn("ToWebmSafe failed", "error", err, "output", string(out))
		return "", err
	}
	return pathOut, nil
}

// ToGif converts a video file to GIF format (for download/preview features).
// It uses FFmpeg's palette-based conversion for better quality, then
// optimizes with gifsicle if available.
func ToGif(f string) (string, error) {
	pathOut := f + ".gif"
	bin := ffmpegBin

	var args []string
	// Use VP9 decoder for .webm input.
	if strings.HasSuffix(f, ".webm") {
		args = append(args, "-c:v", "libvpx-vp9")
	}
	args = append(args, "-i", f, "-hide_banner",
		"-lavfi", "split[a][b];[a]palettegen[p];[b][p]paletteuse=dither=atkinson",
		"-gifflags", "-transdiff", "-gifflags", "-offsetting",
		"-loglevel", "error", "-y", pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		slog.Warn("ToGif failed", "error", err, "output", string(out))
		return "", err
	}

	// Optimize GIF with gifsicle (best effort, ignore errors).
	if optimizeOut, optimizeErr := exec.Command("gifsicle", "--batch", "-O2", "--lossy=60", pathOut).CombinedOutput(); optimizeErr != nil {
		slog.Debug("gifsicle optimization failed (non-fatal)", "error", optimizeErr, "output", string(optimizeOut))
	}

	return pathOut, nil
}

// SmartConvert detects whether the input is static or animated, then converts accordingly.
// It uses ImageMagick's identify to count frames.
func SmartConvert(f string, isCustomEmoji bool) (string, error) {
	bin := identifyBin
	args := append([]string{}, identifyArgs...)
	args = append(args, "-format", "%n", f)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		slog.Warn("SmartConvert identify failed", "error", err, "output", string(out))
		return "", err
	}

	// ImageMagick may return very large frame counts for certain formats.
	// Truncate to avoid integer overflow.
	outStr := strings.TrimSpace(string(out))
	if len(outStr) > 5 {
		outStr = outStr[:3]
	}

	frameCount, err := strconv.Atoi(outStr)
	if err != nil {
		slog.Warn("SmartConvert: failed to parse frame count", "error", err, "raw", outStr)
		return "", err
	}

	if frameCount == 0 {
		slog.Warn("SmartConvert: frame count is zero", "file", f)
		return "", errors.New("frame count is zero")
	}

	if frameCount > 1 {
		return ToWebmVideo(f, isCustomEmoji)
	}
	return ToWebpStatic(f, isCustomEmoji)
}

// GuessFormat guesses the sticker format based on file extension.
// Returns "static", "video", or "animated".
func GuessFormat(f string) string {
	f = strings.ToLower(f)
	videoExts := []string{".webm", ".mp4", ".mov", ".avi", ".mkv", ".gif", ".apng"}
	for _, ext := range videoExts {
		if strings.HasSuffix(f, ext) {
			return "video"
		}
	}
	animatedExts := []string{".tgs", ".lottie", ".json"}
	for _, ext := range animatedExts {
		if strings.HasSuffix(f, ext) {
			return "animated"
		}
	}
	return "static"
}

// GuessIsArchive checks if the file appears to be a compressed archive
// based on its extension.
func GuessIsArchive(f string) bool {
	f = strings.ToLower(f)
	archiveExts := []string{".rar", ".7z", ".zip", ".tar", ".gz", ".bz2", ".zst", ".rar5"}
	for _, ext := range archiveExts {
		if strings.HasSuffix(f, ext) {
			return true
		}
	}
	return false
}

// toApng is an internal helper that converts an image to APNG format.
// Used as an intermediate step when FFmpeg cannot handle animated WEBP directly.
func toApng(f string) (string, error) {
	pathOut := f + ".apng"
	bin := convertBin
	args := append([]string{}, convertArgs...)
	args = append(args, f, pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		slog.Warn("toApng failed", "error", err, "output", string(out))
		return "", err
	}
	return pathOut, nil
}
