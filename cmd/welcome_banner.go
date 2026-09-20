package cmd

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// rhoBlockGlyphs — fixed 8-column ██ font used by the welcome gate banners.
var rhoBlockGlyphs = map[rune][5]string{
	'H': {"██   ██ ", "██   ██ ", "███████ ", "██   ██ ", "██   ██ "},
	'A': {"  ███   ", " █████  ", "███████ ", "██   ██ ", "██   ██ "},
	'W': {"██   ██ ", "██   ██ ", "██ █ ██ ", "███ ███ ", "██   ██ "},
	'K': {"██   ██ ", "██  ██  ", "█████   ", "██  ██  ", "██   ██ "},
	'E': {"███████ ", "██      ", "███████ ", "██      ", "███████ "},
	'L': {"██      ", "██      ", "██      ", "██      ", "███████ "},
	'C': {" ██████ ", "██    ██", "██      ", "██    ██", " ██████ "},
	'O': {" ██████ ", "██    ██", "██    ██", "██    ██", " ██████ "},
	'M': {"██    ██", "██ ██ ██", "██ ██ ██", "██    ██", "██    ██"},
	'T': {"████████", "   ██   ", "   ██   ", "   ██   ", "   ██   "},
}

/* legacyRhoLogoArtLines is retained only as a migration reference.
var legacyRhoLogoArtLines = []string{
	"|    ___    | |    ___    |   (\\              /)   |       \\",
	"|   |   |   | |   |   |   |    \\      \\/      /    |   |\\   \\",
	"|___|   |___| |___|   |___|     \\____/\\/\\____/     |___| \\___\\",
	"                                    |0\\/0|",
	"                                     \\/\\/",
	"                                      \\/",
}
*/

// rhoLogoArtLines is the canonical portable ASCII RHO wordmark.
var rhoLogoArtLines = []string{
	" ____  _   _  ___ ",
	"|  _ \\| | | |/ _ \\ ",
	"| |_) | |_| | | | |",
	"|  _ <|  _  | |_| |",
	"|_| \\_\\_| |_|\\___/",
}

const (
	rhoBlockCellW     = 8
	rhoBlockLetterGap = 1
	rhoBlockWordGap   = 4
)

// welcomeWordLines — "WELCOME" block (row-aligned, fixed grid).
var welcomeWordLines = composeRhoBlockLines("WELCOME")

// welcomeToWordLines — "TO" block, centered under WELCOME on the gate.
var welcomeToWordLines = composeRhoBlockLines("TO")

// welcomeToPhraseLines — "WELCOME TO" block for wide welcome gates.
var welcomeToPhraseLines = composeRhoBlockLines("WELCOME TO")

// welcomeToBannerMinWidth is the visible width for the WELCOME block.
const welcomeToBannerMinWidth = 61

// welcomeToPhraseMinWidth is the visible width for the combined "WELCOME TO" block.
var welcomeToPhraseMinWidth = blockLinesWidth(welcomeToPhraseLines)

func composeRhoBlockLines(text string) []string {
	rows := make([]string, 5)
	words := strings.Fields(text)
	for wi, word := range words {
		for ci, ch := range word {
			glyph, ok := rhoBlockGlyphs[ch]
			if !ok {
				continue
			}
			for i := range rows {
				if rows[i] != "" {
					if ci == 0 && wi > 0 {
						rows[i] += strings.Repeat(" ", rhoBlockWordGap)
					} else {
						rows[i] += strings.Repeat(" ", rhoBlockLetterGap)
					}
				}
				cell := padBlockCell(glyph[i], rhoBlockCellW)
				rows[i] += cell
			}
		}
	}
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return rows
}

func blockLinesWidth(lines []string) int {
	w := 0
	for _, line := range lines {
		if n := runewidth.StringWidth(line); n > w {
			w = n
		}
	}
	return w
}

func padBlockCell(s string, w int) string {
	if runewidth.StringWidth(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-runewidth.StringWidth(s))
}
