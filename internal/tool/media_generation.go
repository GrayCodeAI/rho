package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GrayCodeAI/rho/internal/storage"
)

// MediaEngine is the pluggable backend that actually generates image/video
// assets. rho does not bundle a media provider; a host (or a provider-backed
// flux integration) wires one in via SetMediaEngine. The tool and the
// local-persistence contract are provider-agnostic, so generation, saving, and
// result reporting all work the same regardless of backend.
type MediaEngine interface {
	// Name returns the engine identifier for provenance.
	Name() string
	// GenerateImage produces one or more images from prompt (and optional
	// source for image editing). Each result must carry the media bytes or a
	// URL the host can download; the tool handles persistence.
	GenerateImage(ctx context.Context, prompt, source string, opts MediaOptions) ([]MediaResult, error)
	// GenerateVideo produces a video from prompt (and optional source for
	// image-to-video).
	GenerateVideo(ctx context.Context, prompt, source string, opts MediaOptions) ([]MediaResult, error)
}

// MediaResult is one generated asset as returned by an engine.
type MediaResult struct {
	// Data holds the raw bytes when the engine produces them locally. If empty,
	// the tool attempts to download from URL.
	Data []byte `json:"-"`
	// URL is the provider-hosted location of the asset (used when Data is
	// empty, and preserved for provenance).
	URL string `json:"url,omitempty"`
	// Kind is "image" or "video".
	Kind string `json:"kind"`
	// MIME is the asset MIME type used to pick the file extension.
	MIME string `json:"mime,omitempty"`
}

// MediaOptions carries provider-neutral generation controls.
type MediaOptions struct {
	AspectRatio string `json:"aspect_ratio,omitempty"`
	Resolution  string `json:"resolution,omitempty"` // image: 1k/2k; video: 480p/720p
	Count       int    `json:"count,omitempty"`      // number of images
	DurationSec int    `json:"duration_seconds,omitempty"`
}

// GenerateMediaInput is the typed input for GenerateMediaTool.
type GenerateMediaInput struct {
	Kind        string `json:"kind"`
	Prompt      string `json:"prompt"`
	Source      string `json:"source"`
	AspectRatio string `json:"aspect_ratio"`
	Resolution  string `json:"resolution"`
	Count       int    `json:"count"`
	DurationSec int    `json:"duration_seconds"`
	OutputPath  string `json:"output_path"`
}

// MediaAsset is the persisted, locally-available representation returned to the
// model and user.
type MediaAsset struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	MIME   string `json:"mime,omitempty"`
	Prompt string `json:"prompt,omitempty"`
	URL    string `json:"url,omitempty"`
}

var mediaEngine MediaEngine

// SetMediaEngine installs the media-generation backend. It is nil by default;
// the GenerateMedia tool reports a clear error until a host wires one in.
func SetMediaEngine(e MediaEngine) { mediaEngine = e }

// MediaEngineName returns the active engine name, or "" when none is installed.
func MediaEngineName() string {
	if mediaEngine == nil {
		return ""
	}
	return mediaEngine.Name()
}

// DefaultMediaDir returns the stable, user-scoped directory for generated media
// (the rho analog of grok-cli's .grok/generated-media).
func DefaultMediaDir() string {
	return filepath.Join(storage.StateDir(), "generated-media")
}

// GenerateMediaTool creates images/videos via a pluggable media engine and
// persists outputs locally so they remain usable after provider URLs expire.
// Adopted from grok-cli's generate_image/generate_video tools.
type GenerateMediaTool struct{}

func (GenerateMediaTool) Name() string      { return "GenerateMedia" }
func (GenerateMediaTool) RiskLevel() string { return "medium" }
func (GenerateMediaTool) Aliases() []string { return []string{"generate-media", "media"} }
func (GenerateMediaTool) Description() string {
	return "Generate an image or short video from a text prompt (and optionally edit an existing local image or URL). The generated asset is saved locally and its path is returned so you can reference it directly."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (GenerateMediaTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"kind":             {Type: "string", Enum: []interface{}{"image", "video"}, Description: "The kind of media to generate."},
			"prompt":           {Type: "string", Description: "Text description of the media to generate."},
			"source":           {Type: "string", Description: "Optional local file path or URL used for image editing / image-to-video."},
			"aspect_ratio":     {Type: "string", Description: "Aspect ratio, e.g. 16:9, 1:1, 9:16."},
			"resolution":       {Type: "string", Description: "Resolution: images 1k or 2k; video 480p or 720p."},
			"count":            {Type: "integer", Minimum: 1, Maximum: 4, Description: "Number of images to generate (default 1)."},
			"duration_seconds": {Type: "integer", Minimum: 1, Maximum: 15, Description: "Video duration in seconds (default 5)."},
			"output_path":      {Type: "string", Description: "Optional explicit output directory; defaults to the user state generated-media directory."},
		},
		Required: []string{"kind", "prompt"},
	}
}

func (GenerateMediaTool) Parameters() map[string]interface{} {
	return generateMediaSchema.ToJSONSchema()
}

// generateMediaSchema is the single source of truth for GenerateMedia's input schema.
var generateMediaSchema = GenerateMediaTool{}.Schema()

func (GenerateMediaTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[GenerateMediaInput]("GenerateMedia", input)
	if err != nil {
		return "", err
	}
	p.Kind = strings.ToLower(strings.TrimSpace(p.Kind))
	p.Prompt = strings.TrimSpace(p.Prompt)
	if p.Kind != "image" && p.Kind != "video" {
		return "", fmt.Errorf("kind must be image or video")
	}
	if p.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if mediaEngine == nil {
		return "", fmt.Errorf("no media engine installed — configure a provider-backed media backend first")
	}
	if p.Count <= 0 {
		p.Count = 1
	}
	if p.Count > 4 {
		p.Count = 4
	}
	if p.DurationSec <= 0 {
		p.DurationSec = 5
	}
	if p.DurationSec > 15 {
		p.DurationSec = 15
	}

	source := p.Source
	if source != "" {
		// Resolve a local source path to an absolute path (URLs pass through).
		if !strings.Contains(source, "://") {
			if err := validatePathAllowed(ctx, source); err != nil {
				return "", err
			}
			abs, err := filepath.Abs(source)
			if err != nil {
				return "", fmt.Errorf("resolve source: %w", err)
			}
			source = abs
		}
	}

	opts := MediaOptions{
		AspectRatio: p.AspectRatio,
		Resolution:  p.Resolution,
		Count:       p.Count,
		DurationSec: p.DurationSec,
	}

	var results []MediaResult
	switch p.Kind {
	case "image":
		results, err = mediaEngine.GenerateImage(ctx, p.Prompt, source, opts)
	case "video":
		results, err = mediaEngine.GenerateVideo(ctx, p.Prompt, source, opts)
	}
	if err != nil {
		return "", fmt.Errorf("media generation failed: %w", err)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("media engine returned no assets")
	}

	dest := p.OutputPath
	if dest == "" {
		dest = DefaultMediaDir()
	}
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return "", fmt.Errorf("create media dir: %w", err)
	}

	assets := make([]MediaAsset, 0, len(results))
	for i, r := range results {
		asset, err := persistMediaResult(ctx, dest, p.Kind, r, i)
		if err != nil {
			return "", err
		}
		asset.Prompt = p.Prompt
		assets = append(assets, asset)
	}
	return encodeJSON(map[string]interface{}{"engine": mediaEngine.Name(), "assets": assets})
}

// persistMediaResult saves one engine result to dest and returns the asset
// descriptor. Bytes come from the engine; otherwise the URL is downloaded.
func persistMediaResult(ctx context.Context, dest, kind string, r MediaResult, index int) (MediaAsset, error) {
	ext := extensionForMIME(r.MIME, kind)
	name := fmt.Sprintf("media-%s-%d%s", time.Now().UTC().Format("20060102-150405"), index, ext)
	path := filepath.Join(dest, name)

	if len(r.Data) == 0 && r.URL != "" {
		data, err := downloadMedia(ctx, r.URL)
		if err != nil {
			return MediaAsset{}, fmt.Errorf("download %s: %w", r.URL, err)
		}
		r.Data = data
	}
	if len(r.Data) == 0 {
		return MediaAsset{}, fmt.Errorf("media result has neither data nor a downloadable URL")
	}
	if err := os.WriteFile(path, r.Data, 0o644); err != nil { // #nosec G306 -- generated media is intentionally world-readable
		return MediaAsset{}, fmt.Errorf("write media: %w", err)
	}
	return MediaAsset{
		Path: path,
		Kind: kind,
		MIME: r.MIME,
		URL:  r.URL,
	}, nil
}

// extensionForMIME maps a media MIME type to a file extension, defaulting by kind.
func extensionForMIME(mime, kind string) string {
	switch strings.ToLower(mime) {
	case "image/png", "image/png; charset=utf-8":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "video/mp4", "video/webm", "video/quicktime":
		ext := ".mp4"
		if strings.Contains(strings.ToLower(mime), "webm") {
			ext = ".webm"
		}
		if strings.Contains(strings.ToLower(mime), "quicktime") {
			ext = ".mov"
		}
		return ext
	}
	if kind == "video" {
		return ".mp4"
	}
	return ".png"
}

// downloadMedia fetches a provider-hosted media URL. Only http(s) is allowed.
func downloadMedia(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	// Bound the download so a slow or hostile host cannot hang the tool.
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 50<<20)) // 50 MB cap
}
