package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestDetectCapability(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		supported bool
		terminal  string
	}{
		{name: "kitty pid", env: map[string]string{"KITTY_PID": "1234"}, supported: true, terminal: "kitty"},
		{name: "kitty window", env: map[string]string{"KITTY_WINDOW_ID": "0"}, supported: true, terminal: "kitty"},
		{name: "ghostty", env: map[string]string{"GHOSTTY_RESOURCES_DIR": "/x"}, supported: true, terminal: "ghostty"},
		{name: "term program kitty", env: map[string]string{"TERM_PROGRAM": "Kitty"}, supported: true, terminal: "kitty"},
		{name: "term program ghostty", env: map[string]string{"TERM_PROGRAM": "ghostty"}, supported: true, terminal: "ghostty"},
		{name: "iterm not supported", env: map[string]string{"TERM_PROGRAM": "iTerm.app"}, supported: false, terminal: "iterm2"},
		{name: "generic not supported", env: map[string]string{}, supported: false, terminal: "generic"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Clear every variable detectTerminal consults so the host
			// environment (e.g. a Ghostty or Kitty session running the tests)
			// cannot leak into the probe.
			for _, k := range []string{"KITTY_PID", "KITTY_WINDOW_ID", "GHOSTTY_RESOURCES_DIR", "TERM_PROGRAM"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			c := DetectCapability()
			if c.Supported != tc.supported {
				t.Errorf("Supported = %v, want %v", c.Supported, tc.supported)
			}
			if c.Terminal != tc.terminal {
				t.Errorf("Terminal = %q, want %q", c.Terminal, tc.terminal)
			}
		})
	}
}

// golden prefix for a chunked PNG transmission: APC start, a=T, format 100 (PNG).
const pngTransmissionPrefix = "\x1b_Ga=T,f=100"

func TestEncodePNGGoldenPrefix(t *testing.T) {
	// A minimal 1x1 PNG (well-formed enough to exercise encoding; exact raster
	// content is irrelevant to the protocol framing).
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}
	seq := EncodePNG(png, 1, 1)
	if !strings.HasPrefix(seq, pngTransmissionPrefix) {
		t.Fatalf("EncodePNG prefix = %q, want prefix %q", seq, pngTransmissionPrefix)
	}
	// The payload must be base64 of the PNG bytes and the sequence must close
	// with the APC terminator.
	if !strings.HasSuffix(seq, "\x1b\\") {
		t.Errorf("EncodePNG must end with APC terminator")
	}
	if !strings.Contains(seq, "s=1,v=1") {
		t.Errorf("EncodePNG must declare dimensions, got %q", seq)
	}
}

func TestEncodePNGChunkedLarge(t *testing.T) {
	// A payload large enough to require multiple chunks.
	png := bytes.Repeat([]byte{0xAB}, 10*1024)
	seq := EncodePNG(png, 64, 64)
	// Chunked transmissions contain an intermediate m=1 chunk before the final m=0.
	if !strings.Contains(seq, "m=1;") {
		t.Errorf("expected an intermediate m=1 chunk for large payload")
	}
	if !strings.Contains(seq, "m=0;") {
		t.Errorf("expected final m=0 chunk")
	}
}

func TestEmitPNGFallsBackWhenUnsupported(t *testing.T) {
	// Force a non-capable terminal.
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	// Clear any kitty/ghostty env that the test runner may have inherited.
	t.Setenv("KITTY_PID", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("GHOSTTY_RESOURCES_DIR", "")

	var buf bytes.Buffer
	emitted, err := EmitPNG(&buf, []byte("png"), 1, 1)
	if err != nil {
		t.Fatalf("EmitPNG error: %v", err)
	}
	if emitted {
		t.Error("EmitPNG must not emit on an unsupported terminal")
	}
	if buf.Len() != 0 {
		t.Errorf("EmitPNG wrote %d bytes on unsupported terminal, want 0", buf.Len())
	}
}

func TestEmitPNGEmittedWhenSupported(t *testing.T) {
	t.Setenv("KITTY_PID", "1234")
	var buf bytes.Buffer
	emitted, err := EmitPNG(&buf, []byte{0x89, 'P', 'N', 'G'}, 1, 1)
	if err != nil {
		t.Fatalf("EmitPNG error: %v", err)
	}
	if !emitted {
		t.Error("EmitPNG must emit on a kitty terminal")
	}
	if !strings.HasPrefix(buf.String(), pngTransmissionPrefix) {
		t.Errorf("EmitPNG output prefix = %q, want %q", buf.String(), pngTransmissionPrefix)
	}
}

func TestEmitPNGOversizeFallsBack(t *testing.T) {
	t.Setenv("KITTY_PID", "1234")
	var buf bytes.Buffer
	emitted, err := EmitPNG(&buf, []byte("png"), MaxDimension+1, 1)
	if err != nil {
		t.Fatalf("EmitPNG error: %v", err)
	}
	if emitted {
		t.Error("EmitPNG must not emit an oversize image")
	}
	if buf.Len() != 0 {
		t.Errorf("EmitPNG wrote bytes for oversize image, want 0")
	}
}

func TestClearGraphicsEmitsKittyDeleteAll(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "kitty")
	var buf bytes.Buffer
	if err := ClearGraphics(&buf); err != nil {
		t.Fatalf("ClearGraphics returned error: %v", err)
	}
	if got, want := buf.String(), "\x1b_Ga=d,d=A\x1b\\"; got != want {
		t.Fatalf("ClearGraphics output = %q, want %q", got, want)
	}
}
