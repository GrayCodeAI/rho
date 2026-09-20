package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	rhoconfig "github.com/GrayCodeAI/rho/internal/config"
)

// AdditionalDirContext validates a directory and formats its AGENTS.md
// instructions for inclusion in a conversation.
func AdditionalDirContext(dir string) (absolutePath, contextBlock string, err error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", "", fmt.Errorf("directory path is required")
	}
	absolutePath, err = filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(absolutePath)
	if err != nil {
		return "", "", err
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("%s is not a directory", absolutePath)
	}

	var b strings.Builder
	b.WriteString("Additional directory: " + absolutePath)
	if md := rhoconfig.LoadAgentsMDFrom(absolutePath); md != "" {
		b.WriteString("\nAdditional directory instructions (" + absolutePath + "):\n" + md)
	}
	return absolutePath, b.String(), nil
}
