// theme_palettes.go — All registered theme palettes.
//
// Each palette defines raw hex colors for a complete theme. To add a theme,
// add a new palette and register it in themeRegistry.

package theme

import "strings"

// darkPalette is the default dark theme with Rho's Talon Gold accent.
var darkPalette = Palette{
	Panel:     "#1b1e26",
	PromptBg:  "#262626",
	Line:      "#242429",
	Line2:     "#414147",
	Ink:       "#ececee",
	Muted:     "#9a9aa2",
	Faint:     "#8a8a92",
	Faintest:  "#7c7c82",
	Accent:    BrandPrimary,
	Green:     "#5dd1a4",
	Red:       "#ff7a7a",
	Amber:     "#ffc25c",
	Blue:      "#7db4ff",
	GitAdd:    "#7db87a",
	GitDel:    "#b87a7a",
	AddBg:     "#18352c",
	DelBg:     "#241819",
	AddBgWord: "#2e654d",
	DelBgWord: "#502d30",
	PermBg:    "#1c1915",
	SelBg:     "#32401b",
	AddInk:    "#bdeed7",
	DelInk:    "#f2c4c4",
	OnAccent:  "#000000",
	CardRun:   "#5a6b2e",
	CardErr:   "#6b3434",
	CardPerm:  "#6b5a2e",
}

// draculaPalette — the Dracula scheme with muted violet surface.
var draculaPalette = Palette{
	Panel:     "#282a36",
	PromptBg:  "#383c4d",
	Line:      "#363a4b",
	Line2:     "#484c62",
	Ink:       "#f8f8f2",
	Muted:     "#b9bccb",
	Faint:     "#a2a5b8",
	Faintest:  "#9195ac",
	Accent:    "#bd93f9",
	Green:     "#50fa7b",
	Red:       "#ff5555",
	Amber:     "#ffb86c",
	Blue:      "#8be9fd",
	GitAdd:    "#77c58c",
	GitDel:    "#d98d8d",
	AddBg:     "#1c3b2a",
	DelBg:     "#3a2026",
	AddBgWord: "#235035",
	DelBgWord: "#5e333b",
	PermBg:    "#322a1e",
	SelBg:     "#504482",
	AddInk:    "#cbf2dd",
	DelInk:    "#f4c9c9",
	OnAccent:  "#000000",
	CardRun:   "#7c6aa6",
	CardErr:   "#98505a",
	CardPerm:  "#9a7c62",
}

// nordPalette — cool polar-night slate with a frost-blue accent.
var nordPalette = Palette{
	Panel:     "#3b4252",
	PromptBg:  "#464f62",
	Line:      "#434c5e",
	Line2:     "#4c566a",
	Ink:       "#eceff4",
	Muted:     "#c8cfda",
	Faint:     "#b4bdcb",
	Faintest:  "#a5afc1",
	Accent:    "#88c0d0",
	Green:     "#a3be8c",
	Red:       "#bf616a",
	Amber:     "#d08770",
	Blue:      "#81a1c1",
	GitAdd:    "#8ba077",
	GitDel:    "#b0757d",
	AddBg:     "#37433a",
	DelBg:     "#45383d",
	AddBgWord: "#456d46",
	DelBgWord: "#6d4650",
	PermBg:    "#47413a",
	SelBg:     "#40688a",
	AddInk:    "#d6ecca",
	DelInk:    "#f0c6cb",
	OnAccent:  "#000000",
	CardRun:   "#4c6672",
	CardErr:   "#6b4a51",
	CardPerm:  "#6a564d",
}

// gruvboxPalette — warm retro browns with an olive-green accent.
var gruvboxPalette = Palette{
	Panel:     "#32302f",
	PromptBg:  "#3c3836",
	Line:      "#504945",
	Line2:     "#665c54",
	Ink:       "#ebdbb2",
	Muted:     "#c9b99a",
	Faint:     "#b7a78d",
	Faintest:  "#a89984",
	Accent:    "#8ec07c",
	Green:     "#b8bb26",
	Red:       "#fb4934",
	Amber:     "#fabd2f",
	Blue:      "#83a598",
	GitAdd:    "#98971a",
	GitDel:    "#cc241d",
	AddBg:     "#2f3a29",
	DelBg:     "#3b2b29",
	AddBgWord: "#3e5236",
	DelBgWord: "#593733",
	PermBg:    "#38331e",
	SelBg:     "#3d4e30",
	AddInk:    "#c5e6b0",
	DelInk:    "#f0c0bb",
	OnAccent:  "#000000",
	CardRun:   "#6f8460",
	CardErr:   "#a5493d",
	CardPerm:  "#a5833a",
}

// tokyoNightPalette — deep indigo surface with soft blue accent.
var tokyoNightPalette = Palette{
	Panel:     "#1e2030",
	PromptBg:  "#2c3149",
	Line:      "#262a3d",
	Line2:     "#3b4261",
	Ink:       "#c8d3f5",
	Muted:     "#a9b1d0",
	Faint:     "#9099b2",
	Faintest:  "#838ba8",
	Accent:    "#82aaff",
	Green:     "#c3e88d",
	Red:       "#ff757f",
	Amber:     "#ffc777",
	Blue:      "#86e1fc",
	GitAdd:    "#96bf7d",
	GitDel:    "#c77e85",
	AddBg:     "#20303b",
	DelBg:     "#37222c",
	AddBgWord: "#2b5a4a",
	DelBgWord: "#5c2e3a",
	PermBg:    "#2a2419",
	SelBg:     "#2a385b",
	AddInk:    "#b8e4d3",
	DelInk:    "#f3c4cb",
	OnAccent:  "#000000",
	CardRun:   "#4b5d8b",
	CardErr:   "#7d4857",
	CardPerm:  "#7d6954",
}

// catppuccinPalette — soft lavender surface with mauve accent.
var catppuccinPalette = Palette{
	Panel:     "#1e1e2e",
	PromptBg:  "#34364b",
	Line:      "#313244",
	Line2:     "#45475a",
	Ink:       "#cdd6f4",
	Muted:     "#a6adc8",
	Faint:     "#9399b2",
	Faintest:  "#83889f",
	Accent:    "#cba6f7",
	Green:     "#a6e3a1",
	Red:       "#f38ba8",
	Amber:     "#f9e2af",
	Blue:      "#89b4fa",
	GitAdd:    "#8cbf8a",
	GitDel:    "#cc8a9b",
	AddBg:     "#24312b",
	DelBg:     "#3c2a32",
	AddBgWord: "#2f5140",
	DelBgWord: "#56333f",
	PermBg:    "#29261b",
	SelBg:     "#322e46",
	AddInk:    "#c4ecd6",
	DelInk:    "#f4cdd6",
	OnAccent:  "#000000",
	CardRun:   "#7a6a99",
	CardErr:   "#8a5b72",
	CardPerm:  "#8d8274",
}

// oneDarkPalette — slate-gray surface with a blue accent.
var oneDarkPalette = Palette{
	Panel:     "#2e323b",
	PromptBg:  "#3a3f4a",
	Line:      "#393f4a",
	Line2:     "#4b525f",
	Ink:       "#abb2bf",
	Muted:     "#a2a9b6",
	Faint:     "#9aa1af",
	Faintest:  "#969cab",
	Accent:    "#61afef",
	Green:     "#98c379",
	Red:       "#e06c75",
	Amber:     "#e5c07b",
	Blue:      "#56b6c2",
	GitAdd:    "#82a06a",
	GitDel:    "#bd8087",
	AddBg:     "#2c382b",
	DelBg:     "#3a2d2f",
	AddBgWord: "#3d5a3a",
	DelBgWord: "#5c3e40",
	PermBg:    "#3c3826",
	SelBg:     "#354256",
	AddInk:    "#cdeab3",
	DelInk:    "#f0c3c7",
	OnAccent:  "#000000",
	CardRun:   "#496c8c",
	CardErr:   "#7c515b",
	CardPerm:  "#7e735e",
}

// solarizedDarkPalette — signature teal base03/base02 surface with cyan accent.
var solarizedDarkPalette = Palette{
	Panel:     "#073642",
	PromptBg:  "#0b3b46",
	Line:      "#123f48",
	Line2:     "#4b636c",
	Ink:       "#cdd6d6",
	Muted:     "#a9b3b3",
	Faint:     "#9ba5a5",
	Faintest:  "#929c9c",
	Accent:    "#3bb3a6",
	Green:     "#859900",
	Red:       "#dc322f",
	Amber:     "#b58900",
	Blue:      "#268bd2",
	GitAdd:    "#93a05e",
	GitDel:    "#c67b71",
	AddBg:     "#123f31",
	DelBg:     "#45302e",
	AddBgWord: "#1e5c44",
	DelBgWord: "#6a3f3a",
	PermBg:    "#2e2a18",
	SelBg:     "#17505a",
	AddInk:    "#c3ecd6",
	DelInk:    "#f2cbc6",
	OnAccent:  "#000000",
	CardRun:   "#1d6b6c",
	CardErr:   "#6d393d",
	CardPerm:  "#5b6028",
}

// rosePinePalette — muted rose-quartz base with soft-rose accent.
var rosePinePalette = Palette{
	Panel:     "#1f1d2e",
	PromptBg:  "#2f2b47",
	Line:      "#2b2840",
	Line2:     "#403d52",
	Ink:       "#e0def4",
	Muted:     "#a8a3c0",
	Faint:     "#928ea9",
	Faintest:  "#8985a0",
	Accent:    "#ebbcba",
	Green:     "#31748f",
	Red:       "#eb6f92",
	Amber:     "#f6c177",
	Blue:      "#9ccfd8",
	GitAdd:    "#5e88a2",
	GitDel:    "#d589a7",
	AddBg:     "#1f2d3a",
	DelBg:     "#3a1f2d",
	AddBgWord: "#274b5e",
	DelBgWord: "#763a4f",
	PermBg:    "#4a3e3d",
	SelBg:     "#44415a",
	AddInk:    "#cfe8ef",
	DelInk:    "#f6c9cd",
	OnAccent:  "#000000",
	CardRun:   "#7c6673",
	CardErr:   "#7c4662",
	CardPerm:  "#806857",
}

// everforestPalette — warm forest-gray surface with a sage-green accent.
var everforestPalette = Palette{
	Panel:     "#333c43",
	PromptBg:  "#3d484d",
	Line:      "#414b52",
	Line2:     "#55636b",
	Ink:       "#d3c6aa",
	Muted:     "#b0bab0",
	Faint:     "#a4aea3",
	Faintest:  "#9ca99b",
	Accent:    "#a7c080",
	Green:     "#83c092",
	Red:       "#e67e80",
	Amber:     "#dbbc7f",
	Blue:      "#7fbbb3",
	GitAdd:    "#8faa78",
	GitDel:    "#c08888",
	AddBg:     "#2c3f37",
	DelBg:     "#3e3234",
	AddBgWord: "#324e3b",
	DelBgWord: "#573a3c",
	PermBg:    "#3c382d",
	SelBg:     "#3b482e",
	AddInk:    "#cfead0",
	DelInk:    "#f4cfcd",
	OnAccent:  "#000000",
	CardRun:   "#798b6b",
	CardErr:   "#9c676b",
	CardPerm:  "#96896b",
}

// lightPalette — dark-on-light: warm cream surface with near-black ink.
var lightPalette = Palette{
	Panel:     "#efebd4",
	PromptBg:  "#e3ddc2",
	Line:      "#d8d2bd",
	Line2:     "#b7b199",
	Ink:       "#22201a",
	Muted:     "#4b5149",
	Faint:     "#575e55",
	Faintest:  "#636a61",
	Accent:    "#4b6508",
	Green:     "#1e725c",
	Red:       "#c02434",
	Amber:     "#8a5f00",
	Blue:      "#1f66c0",
	GitAdd:    "#3d6f46",
	GitDel:    "#a34a4a",
	AddBg:     "#ddf0df",
	DelBg:     "#f8dcdc",
	AddBgWord: "#a2daae",
	DelBgWord: "#f2b6b6",
	PermBg:    "#f7ebc6",
	SelBg:     "#d4e08f",
	AddInk:    "#0c4026",
	DelInk:    "#641a1d",
	OnAccent:  "#ffffff",
	CardRun:   "#b0be7e",
	CardErr:   "#d8b0a8",
	CardPerm:  "#d6c496",
}

// solarizedLightPalette — base3/base2 cream surface with fixed accent wheel.
var solarizedLightPalette = Palette{
	Panel:     "#eee8d5",
	PromptBg:  "#e1d9be",
	Line:      "#d8d1bc",
	Line2:     "#c0b89e",
	Ink:       "#304049",
	Muted:     "#495b61",
	Faint:     "#506469",
	Faintest:  "#576b72",
	Accent:    "#0c665c",
	Green:     "#859900",
	Red:       "#dc322f",
	Amber:     "#7a5c00",
	Blue:      "#268bd2",
	GitAdd:    "#788d34",
	GitDel:    "#ac4f50",
	AddBg:     "#dde8c6",
	DelBg:     "#f1ddd2",
	AddBgWord: "#b9d488",
	DelBgWord: "#edc4b4",
	PermBg:    "#f0e6bd",
	SelBg:     "#a6d6c4",
	AddInk:    "#38480a",
	DelInk:    "#6f1614",
	OnAccent:  "#ffffff",
	CardRun:   "#7fbaaf",
	CardErr:   "#d8837a",
	CardPerm:  "#c4ae63",
}

// monokaiPalette — the classic high-contrast Monokai color scheme.
var monokaiPalette = Palette{
	Panel:     "#272822",
	PromptBg:  "#383830",
	Line:      "#3e3d32",
	Line2:     "#49483e",
	Ink:       "#f8f8f2",
	Muted:     "#c2c0b2",
	Faint:     "#a8a697",
	Faintest:  "#908e80",
	Accent:    "#a6e22e",
	Green:     "#a6e22e",
	Red:       "#f92672",
	Amber:     "#e6db74",
	Blue:      "#66d9e8",
	GitAdd:    "#86c42e",
	GitDel:    "#d9506a",
	AddBg:     "#1d3010",
	DelBg:     "#3d1020",
	AddBgWord: "#2e5018",
	DelBgWord: "#601530",
	PermBg:    "#2a2518",
	SelBg:     "#49483e",
	AddInk:    "#d4f4a6",
	DelInk:    "#fbc5d2",
	OnAccent:  "#000000",
	CardRun:   "#5a7828",
	CardErr:   "#7a283c",
	CardPerm:  "#78722a",
}

// kanagawaPalette — inspired by Hokusai's "The Great Wave", cool deep navy.
var kanagawaPalette = Palette{
	Panel:     "#1f1f28",
	PromptBg:  "#2a2a37",
	Line:      "#2d2d3b",
	Line2:     "#363646",
	Ink:       "#dcd7ba",
	Muted:     "#b8b4a0",
	Faint:     "#9e9a88",
	Faintest:  "#868274",
	Accent:    "#7e9cd8",
	Green:     "#98bb6c",
	Red:       "#e46876",
	Amber:     "#dca561",
	Blue:      "#7fb4ca",
	GitAdd:    "#82a35a",
	GitDel:    "#c46070",
	AddBg:     "#1a2e18",
	DelBg:     "#2e1820",
	AddBgWord: "#284524",
	DelBgWord: "#4a2030",
	PermBg:    "#201e16",
	SelBg:     "#2d4f67",
	AddInk:    "#cde8b6",
	DelInk:    "#f4bec6",
	OnAccent:  "#000000",
	CardRun:   "#3e5578",
	CardErr:   "#6a3840",
	CardPerm:  "#6a5230",
}

// ayuPalette — Ayu Mirage variant: warm dark with vivid orange accent.
var ayuPalette = Palette{
	Panel:     "#1f2430",
	PromptBg:  "#232834",
	Line:      "#2a2f3d",
	Line2:     "#343a4c",
	Ink:       "#cbccc6",
	Muted:     "#a8a9a5",
	Faint:     "#8e8f8c",
	Faintest:  "#787973",
	Accent:    "#ffad66",
	Green:     "#bae67e",
	Red:       "#f28779",
	Amber:     "#ffd580",
	Blue:      "#5ccfe6",
	GitAdd:    "#92ba60",
	GitDel:    "#d07068",
	AddBg:     "#1c2e14",
	DelBg:     "#2e1e1e",
	AddBgWord: "#2a4820",
	DelBgWord: "#4a2424",
	PermBg:    "#1e1c14",
	SelBg:     "#343f5e",
	AddInk:    "#d4efb2",
	DelInk:    "#f8c8c4",
	OnAccent:  "#000000",
	CardRun:   "#88613e",
	CardErr:   "#6a3a38",
	CardPerm:  "#6a5e30",
}

// paleNightPalette — Material Palenight: deep blue-purple with a vibrant cyan accent.
var paleNightPalette = Palette{
	Panel:     "#292d3e",
	PromptBg:  "#333747",
	Line:      "#3a3f52",
	Line2:     "#444a5e",
	Ink:       "#a6accd",
	Muted:     "#8a90b2",
	Faint:     "#727898",
	Faintest:  "#5e647e",
	Accent:    "#82aaff",
	Green:     "#c3e88d",
	Red:       "#f07178",
	Amber:     "#ffcb6b",
	Blue:      "#89ddff",
	GitAdd:    "#a0c070",
	GitDel:    "#c06068",
	AddBg:     "#1e3020",
	DelBg:     "#301e24",
	AddBgWord: "#2c4e2e",
	DelBgWord: "#4e2634",
	PermBg:    "#26202e",
	SelBg:     "#444a70",
	AddInk:    "#d2f0ac",
	DelInk:    "#f8c0c4",
	OnAccent:  "#000000",
	CardRun:   "#4e5c90",
	CardErr:   "#6e3a42",
	CardPerm:  "#5c4e28",
}

// githubDarkPalette — GitHub's official dark theme palette.
var githubDarkPalette = Palette{
	Panel:     "#0d1117",
	PromptBg:  "#161b22",
	Line:      "#1c2128",
	Line2:     "#21262d",
	Ink:       "#e6edf3",
	Muted:     "#b1bac4",
	Faint:     "#8d96a0",
	Faintest:  "#6e7681",
	Accent:    "#58a6ff",
	Green:     "#3fb950",
	Red:       "#f85149",
	Amber:     "#d29922",
	Blue:      "#79c0ff",
	GitAdd:    "#2ea043",
	GitDel:    "#da3633",
	AddBg:     "#0d2b1a",
	DelBg:     "#2b0e0e",
	AddBgWord: "#124d2a",
	DelBgWord: "#4a1414",
	PermBg:    "#161210",
	SelBg:     "#1f3a5a",
	AddInk:    "#aff5b4",
	DelInk:    "#ffc1bd",
	OnAccent:  "#000000",
	CardRun:   "#1a4a7a",
	CardErr:   "#6a2020",
	CardPerm:  "#5a4a10",
}

// githubLightPalette — GitHub's light theme palette, tuned for terminal
// contrast rather than browser white-space. It complements github-dark while
// keeping the same semantic colors for diffs, errors, and permissions.
var githubLightPalette = Palette{
	Panel:     "#ffffff",
	PromptBg:  "#f6f8fa",
	Line:      "#d0d7de",
	Line2:     "#afb8c1",
	Ink:       "#1f2328",
	Muted:     "#656d76",
	Faint:     "#818b98",
	Faintest:  "#8c959f",
	Accent:    "#0757b8",
	Green:     "#1a7f37",
	Red:       "#cf222e",
	Amber:     "#9a6700",
	Blue:      "#0550ae",
	GitAdd:    "#1a7f37",
	GitDel:    "#cf222e",
	AddBg:     "#dafbe1",
	DelBg:     "#ffebe9",
	AddBgWord: "#aceebb",
	DelBgWord: "#ffcecb",
	PermBg:    "#fff8c5",
	SelBg:     "#ddf4ff",
	AddInk:    "#116329",
	DelInk:    "#82071e",
	OnAccent:  "#ffffff",
	CardRun:   "#b6d7f5",
	CardErr:   "#f2b8b5",
	CardPerm:  "#ead79b",
}

// minimalPalette is a low-visual-noise palette for users who prefer a near-
// unstyled terminal experience. Colors stay close to terminal defaults with
// minimal contrast, reducing visual clutter while remaining readable.
var minimalPalette = Palette{
	Panel:     "#1a1a1a",
	PromptBg:  "#1a1a1a",
	Line:      "#2a2a2a",
	Line2:     "#333333",
	Ink:       "#cccccc",
	Muted:     "#999999",
	Faint:     "#777777",
	Faintest:  "#555555",
	Accent:    "#cccccc",
	Green:     "#99cc99",
	Red:       "#cc9999",
	Amber:     "#cccc99",
	Blue:      "#9999cc",
	GitAdd:    "#99cc99",
	GitDel:    "#cc9999",
	AddBg:     "#1a2a1a",
	DelBg:     "#2a1a1a",
	AddBgWord: "#2a3a2a",
	DelBgWord: "#3a2a2a",
	PermBg:    "#2a2a1a",
	SelBg:     "#2a2a2a",
	AddInk:    "#99cc99",
	DelInk:    "#cc9999",
	OnAccent:  "#1a1a1a",
	CardRun:   "#2a2a3a",
	CardErr:   "#3a2a2a",
	CardPerm:  "#3a3a2a",
}

// tauPalette — the restrained, low-chrome look of Tau: near-black surface,
// a single muted blue accent, generous dim text, and hairline separators.
var tauPalette = Palette{
	Panel:     "#0d1117",
	PromptBg:  "#161b22",
	Line:      "#21262d",
	Line2:     "#30363d",
	Ink:       "#e6edf3",
	Muted:     "#8b949e",
	Faint:     "#6e7681",
	Faintest:  "#484f58",
	Accent:    "#7aa2f7",
	Green:     "#3fb950",
	Red:       "#f85149",
	Amber:     "#d29922",
	Blue:      "#58a6ff",
	GitAdd:    "#3fb950",
	GitDel:    "#f85149",
	AddBg:     "#12261e",
	DelBg:     "#2d1418",
	AddBgWord: "#1f4d33",
	DelBgWord: "#5c2226",
	PermBg:    "#2b2412",
	SelBg:     "#1f2d4d",
	AddInk:    "#7ee2a8",
	DelInk:    "#ffa198",
	OnAccent:  "#0d1117",
	CardRun:   "#1f3a5f",
	CardErr:   "#5c2226",
	CardPerm:  "#4d3f1f",
}

// themeEntry is one registered theme for the theme picker.
type themeEntry struct {
	Name    string
	Label   string
	Palette Palette
	IsDark  bool
}

// themeRegistry is the ordered source of truth for every selectable theme.
// Add a new theme here — nothing else needs editing.
var themeRegistry = []themeEntry{
	{Name: "dark", Label: "Dark", Palette: darkPalette, IsDark: true},
	{Name: "dracula", Label: "Dracula", Palette: draculaPalette, IsDark: true},
	{Name: "nord", Label: "Nord", Palette: nordPalette, IsDark: true},
	{Name: "gruvbox", Label: "Gruvbox", Palette: gruvboxPalette, IsDark: true},
	{Name: "tokyo-night", Label: "Tokyo Night", Palette: tokyoNightPalette, IsDark: true},
	{Name: "catppuccin", Label: "Catppuccin", Palette: catppuccinPalette, IsDark: true},
	{Name: "one-dark", Label: "One Dark", Palette: oneDarkPalette, IsDark: true},
	{Name: "solarized-dark", Label: "Solarized Dark", Palette: solarizedDarkPalette, IsDark: true},
	{Name: "rose-pine", Label: "Rosé Pine", Palette: rosePinePalette, IsDark: true},
	{Name: "everforest", Label: "Everforest", Palette: everforestPalette, IsDark: true},
	{Name: "monokai", Label: "Monokai", Palette: monokaiPalette, IsDark: true},
	{Name: "kanagawa", Label: "Kanagawa", Palette: kanagawaPalette, IsDark: true},
	{Name: "ayu", Label: "Ayu Mirage", Palette: ayuPalette, IsDark: true},
	{Name: "palenight", Label: "Palenight", Palette: paleNightPalette, IsDark: true},
	{Name: "github-dark", Label: "GitHub Dark", Palette: githubDarkPalette, IsDark: true},
	{Name: "github-light", Label: "GitHub Light", Palette: githubLightPalette, IsDark: false},
	{Name: "light", Label: "Light", Palette: lightPalette, IsDark: false},
	{Name: "solarized-light", Label: "Solarized Light", Palette: solarizedLightPalette, IsDark: false},
	{Name: "minimal", Label: "Minimal", Palette: minimalPalette, IsDark: true},
	{Name: "tau", Label: "Tau", Palette: tauPalette, IsDark: true},
}

// themeByName indexes the registry by registered name for O(1) lookup.
// Theme names are already lowercase in the registry; lookups are case-sensitive.
var themeByName = func() map[string]themeEntry {
	byName := make(map[string]themeEntry, len(themeRegistry))
	for _, entry := range themeRegistry {
		byName[entry.Name] = entry
	}
	return byName
}()

// ThemeByName returns the map of all theme entries by name.
func ThemeByName() map[string]themeEntry {
	// Do not expose the registry's mutable backing map. A caller that edits the
	// returned map must not silently change the theme picker for every session.
	returnMap := make(map[string]themeEntry, len(themeByName))
	for name, entry := range themeByName {
		returnMap[name] = entry
	}
	return returnMap
}

// GetThemeEntry returns a theme entry by name.
func GetThemeEntry(name string) themeEntry {
	return themeByName[name]
}

// LookupTheme resolves a theme name case- and space-insensitively.
func LookupTheme(name string) (themeEntry, bool) {
	key := strings.ToLower(strings.TrimSpace(name))
	entry, ok := themeByName[key]
	return entry, ok
}

// ThemeNames returns every registered theme name in registry (picker) order.
func ThemeNames() []string {
	names := make([]string, len(themeRegistry))
	for index, entry := range themeRegistry {
		names[index] = entry.Name
	}
	return names
}

// IsDarkTheme checks if a theme name is a dark theme.
func IsDarkTheme(name string) bool {
	entry, ok := LookupTheme(name)
	if !ok {
		return true // default to dark if theme not found
	}
	return entry.IsDark
}
