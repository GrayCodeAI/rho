// Package tui provides terminal UI helpers for rho. graphics.go adds
// Kitty graphics protocol image display with capability detection and a safe
// text fallback. It is isolated from the Bubble Tea render loop so the escape
// sequences it emits never pass through content sanitization.
package tui

import (
	"io"
	"os"
	"strings"

	"github.com/GrayCodeAI/rho/internal/tui/kitty"
)

// Capability describes whether the current terminal supports Kitty graphics.
type Capability struct {
	// Supported is true only when a known-capable terminal is detected.
	Supported bool
	// Terminal is the detected terminal name (kitty, ghostty, iterm2, generic).
	Terminal string
}

// DetectCapability probes the environment for Kitty graphics support. It is
// conservative: it reports support only for terminals known to implement the
// protocol (kitty and ghostty), and otherwise reports unsupported so callers
// keep their existing text rendering. The probe never queries the terminal
// interactively, so it is safe in non-interactive and CI contexts.
func DetectCapability() Capability {
	term := detectTerminal()
	return Capability{Supported: term == "kitty" || term == "ghostty", Terminal: term}
}

// detectTerminal mirrors the environment-based probe used by the CLI's
// notification layer. It is duplicated here (rather than imported from cmd) to
// keep this package dependency-free and importable by the TUI without a cycle.
func detectTerminal() string {
	if os.Getenv("KITTY_PID") != "" || os.Getenv("KITTY_WINDOW_ID") != "" {
		return "kitty"
	}
	if os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return "ghostty"
	}
	switch strings.ToLower(os.Getenv("TERM_PROGRAM")) {
	case "kitty":
		return "kitty"
	case "ghostty":
		return "ghostty"
	case "iterm.app":
		return "iterm2"
	}
	return "generic"
}

// MaxDimension bounds the largest image side (in pixels) that will be emitted.
// Beyond this the transmission is skipped so callers fall back to text, which
// protects terminals from absurdly large raster transfers. Kitty's chunked
// protocol handles size, but a sane cap keeps the base64 payload bounded.
const MaxDimension = 8192

// EncodePNG renders a PNG image as a chunked Kitty graphics transmission
// sequence (chunks of kitty.DefaultChunkSize). width/height are the pixel
// dimensions. The returned string is ready to write to the terminal.
func EncodePNG(png []byte, width, height int) string {
	return kitty.EncodeFrame(kitty.FormatPNG, width, height, png)
}

// EmitPNG writes a PNG to w using the Kitty graphics protocol when the current
// terminal supports it. It returns (true, nil) when the image was emitted, or
// (false, nil) when the terminal does not support Kitty graphics (or the image
// exceeds MaxDimension) and the caller should fall back to its existing text
// rendering. Write errors are returned so callers can decide whether to
// surface them; emission is best-effort and never panics.
func EmitPNG(w io.Writer, png []byte, width, height int) (bool, error) {
	if !DetectCapability().Supported {
		return false, nil
	}
	if width <= 0 || height <= 0 || width > MaxDimension || height > MaxDimension {
		return false, nil
	}
	if _, err := io.WriteString(w, EncodePNG(png, width, height)); err != nil {
		return true, err
	}
	return true, nil
}

// ClearGraphics removes graphics placed by rho from the terminal. Kitty
// graphics can outlive Bubble Tea's alternate screen, so shutdown must clear
// them before printing the plain-text farewell.
func ClearGraphics(w io.Writer) error {
	if !DetectCapability().Supported {
		return nil
	}
	_, err := io.WriteString(w, "\x1b_Ga=d,d=A\x1b\\")
	return err
}
