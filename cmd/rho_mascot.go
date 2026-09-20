package cmd

import (
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/GrayCodeAI/rho/internal/features/welcome"
)

func rhoMascotEnabled() bool {
	return welcome.Enabled()
}

func emitRhoMascotCmd() tea.Cmd {
	return func() tea.Msg {
		_ = welcome.Emit(os.Stderr)
		return nil
	}
}
