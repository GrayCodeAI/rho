// theme.go — Rho's theme system.
//
// Every color in the TUI is defined here as a palette struct. To change
// a theme, edit theme_palettes.go. To add a new theme, add a palette and
// register it in themeRegistry.
//
// The palette registry is the source of truth for the TUI's color system.

package theme

// BrandPrimary is Rho's fixed Talon Gold identity color. Theme accents may
// vary, but brand-bearing elements use this value across every theme.
const BrandPrimary = "#FFD700"

// BrandANSI is BrandPrimary encoded for hot paths that render raw ANSI.
const BrandANSI = "\033[38;2;255;215;0m"

// Palette holds raw hex color values for a theme.
// All palettes must be hex strings (e.g., "#FFD700").
type Palette struct {
	Panel     string `json:"panel,omitempty"`
	PromptBg  string `json:"prompt_bg,omitempty"`
	Line      string `json:"line,omitempty"`
	Line2     string `json:"line2,omitempty"`
	Ink       string `json:"ink,omitempty"`
	Muted     string `json:"muted,omitempty"`
	Faint     string `json:"faint,omitempty"`
	Faintest  string `json:"faintest,omitempty"`
	Accent    string `json:"accent,omitempty"`
	Green     string `json:"green,omitempty"`
	Red       string `json:"red,omitempty"`
	Amber     string `json:"amber,omitempty"`
	Blue      string `json:"blue,omitempty"`
	GitAdd    string `json:"git_add,omitempty"`
	GitDel    string `json:"git_del,omitempty"`
	AddBg     string `json:"add_bg,omitempty"`
	DelBg     string `json:"del_bg,omitempty"`
	AddBgWord string `json:"add_bg_word,omitempty"`
	DelBgWord string `json:"del_bg_word,omitempty"`
	PermBg    string `json:"perm_bg,omitempty"`
	SelBg     string `json:"sel_bg,omitempty"`
	AddInk    string `json:"add_ink,omitempty"`
	DelInk    string `json:"del_ink,omitempty"`
	OnAccent  string `json:"on_accent,omitempty"`
	CardRun   string `json:"card_run,omitempty"`
	CardErr   string `json:"card_err,omitempty"`
	CardPerm  string `json:"card_perm,omitempty"`
}
