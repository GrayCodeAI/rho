package session

import (
	"fmt"
	"strconv"
	"strings"
)

func ParseExportFormat(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "md", "markdown":
		return "md", true
	case "json":
		return "json", true
	case "txt", "text":
		return "txt", true
	default:
		return "", false
	}
}

func ParsePositiveCount(args []string, defaultValue int) (int, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(args[0])
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("count must be a positive integer")
	}
	return value, nil
}

func ParsePositiveDays(args []string, defaultValue int) int {
	if len(args) == 0 {
		return defaultValue
	}
	value, err := strconv.Atoi(args[0])
	if err != nil || value <= 0 {
		return defaultValue
	}
	return value
}

func ParseForkIndex(args []string, defaultValue int) int {
	if len(args) == 0 {
		return defaultValue
	}
	value, err := strconv.Atoi(args[0])
	if err != nil {
		return defaultValue
	}
	return value
}

func SearchQuery(text string) string {
	return strings.TrimSpace(strings.TrimPrefix(text, "/search"))
}
