// Package welcome owns the optional graphical welcome treatment.
//
// It has no dependency on the CLI composition root. The CLI decides when to
// schedule the feature; this package only answers capability questions and
// emits the asset. Terminals without Kitty-compatible graphics remain fully
// supported by the caller's text fallback.
package welcome

import (
	"bytes"
	_ "embed"
	"image"
	_ "image/png"
	"io"

	"github.com/GrayCodeAI/rho/internal/tui"
)

//go:embed assets/rho-mascot-display.png
var mascotPNG []byte

// Enabled reports whether the active terminal can display the mascot.
func Enabled() bool {
	return tui.DetectCapability().Supported
}

// Emit writes the mascot to w when the active terminal supports it. Emission
// is intentionally best-effort and never turns a rendering limitation into a
// failed TUI startup. The caller owns any UI-command orchestration.
func Emit(w io.Writer) error {
	if !Enabled() {
		return nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(mascotPNG))
	if err != nil {
		return err
	}
	_, err = tui.EmitPNG(w, mascotPNG, cfg.Width, cfg.Height)
	return err
}
