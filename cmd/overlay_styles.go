package cmd

import lipgloss "charm.land/lipgloss/v2"

// Overlay pickers share one visual language. Keep these as functions so a
// live theme switch is reflected immediately instead of leaving old ANSI
// colors captured in package-level styles.
func overlayBoxStyle() lipgloss.Style {
	return paletteBoxStyle()
}

func overlayTitleStyle() lipgloss.Style {
	return paletteTitleStyle()
}

func overlayDimStyle() lipgloss.Style {
	return paletteDimStyle()
}

func overlayItemStyle() lipgloss.Style {
	return paletteItemStyle()
}

func overlaySelectedStyle() lipgloss.Style {
	return paletteSelStyle()
}

func overlayMatchStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(warnAmber).Bold(true)
}

func overlaySearchIconStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(infoSky)
}

func overlaySearchValueStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(textPrimary)
}

func overlaySelectionMarkerStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(rhoColor).Bold(true)
}
