package config

import (
	"context"
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	"github.com/GrayCodeAI/rho/internal/home"
	"github.com/GrayCodeAI/rho/internal/pathsafe"
	"github.com/GrayCodeAI/rho/internal/provider/gateway"
	"github.com/GrayCodeAI/rho/internal/theme"
	"github.com/GrayCodeAI/rho/internal/token"

	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// PathCheckStatus is pass, warn, or fail for one readiness row.
type PathCheckStatus string

const (
	PathPass PathCheckStatus = "pass"
	PathWarn PathCheckStatus = "warn"
	PathFail PathCheckStatus = "fail"
)

// PathCheck is one row in the developer path readiness report.
type PathCheck struct {
	Section  string          `json:"section"`
	Name     string          `json:"name"`
	Status   PathCheckStatus `json:"status"`
	Detail   string          `json:"detail,omitempty"`
	FixHint  string          `json:"fix_hint,omitempty"`
	Blocking bool            `json:"blocking"`
}

// DeveloperPathReport summarizes developer readiness (setup, security, ecosystem).
type DeveloperPathReport struct {
	Checks      []PathCheck `json:"checks"`
	ChatReady   bool        `json:"chat_ready"`
	SecureReady bool        `json:"secure_ready"`
	Ready       bool        `json:"ready"`
	NextStep    string      `json:"next_step,omitempty"`
}

// EvaluateDeveloperPath builds the developer path readiness report.
func EvaluateDeveloperPath(ctx context.Context) DeveloperPathReport {
	if ctx == nil {
		ctx = context.Background()
	}
	var checks []PathCheck

	setup := EvaluateSetup(ctx)
	if setup.HasCredentials {
		checks = append(checks, PathCheck{
			Section: "Setup", Name: "credentials", Status: PathPass,
			Detail: "API key in OS secret store", Blocking: true,
		})
	} else {
		checks = append(checks, PathCheck{
			Section: "Setup", Name: "credentials", Status: PathFail,
			Detail:   "No provider credentials configured",
			FixHint:  "Run rho and /config to paste an API key (or configure Ollama)",
			Blocking: true,
		})
	}

	if setup.HasModel {
		model := strings.TrimSpace(ActiveModel(ctx))
		checks = append(checks, PathCheck{
			Section: "Setup", Name: "model", Status: PathPass,
			Detail: model, Blocking: true,
		})
	} else {
		checks = append(checks, PathCheck{
			Section: "Setup", Name: "model", Status: PathFail,
			Detail:   "No model selected",
			FixHint:  "Run /config and pick a model from the catalog",
			Blocking: true,
		})
	}

	cat := CatalogHealthReport(ctx)
	switch {
	case cat.Exists && cat.Models > 0:
		checks = append(checks, PathCheck{
			Section: "Setup", Name: "catalog", Status: PathPass,
			Detail: fmt.Sprintf("%d models cached", cat.Models),
		})
	case cat.Exists:
		checks = append(checks, PathCheck{
			Section: "Setup", Name: "catalog", Status: PathWarn,
			Detail:  "Catalog file present but empty",
			FixHint: "Run rho models refresh after adding credentials",
		})
	default:
		checks = append(checks, PathCheck{
			Section: "Setup", Name: "catalog", Status: PathWarn,
			Detail:  CatalogEmptyHint(ctx),
			FixHint: "Add credentials then run rho models refresh",
		})
	}

	storageStatus := CredentialStorageStatus(ctx)
	if storageStatus.Writable {
		checks = append(checks, PathCheck{
			Section: "Security", Name: "keychain", Status: PathPass,
			Detail:   CredentialStoreName() + " writable",
			Blocking: true,
		})
	} else {
		checks = append(checks, PathCheck{
			Section: "Security", Name: "keychain", Status: PathWarn,
			Detail:   storageStatus.Detail,
			FixHint:  "Unlock keychain or enable secret service (Linux)",
			Blocking: true,
		})
	}

	if hasSecrets, detail := providerJSONHasSecretsOnDisk(); hasSecrets {
		checks = append(checks, PathCheck{
			Section: "Security", Name: "provider.json", Status: PathFail,
			Detail:   detail,
			FixHint:  "Back up provider.json, remove its secret fields, then save keys via /config",
			Blocking: true,
		})
	} else {
		checks = append(checks, PathCheck{
			Section: "Security", Name: "provider.json", Status: PathPass,
			Detail:   "No API secrets on disk (routing only)",
			Blocking: true,
		})
	}

	rhoDir := home.MustDir()
	provPath := ProviderStateSecurityStatus().Path
	if provPath == "" {
		provPath = filepath.Join(rhoDir, ".rho", "provider.json")
	}
	if reason := pathsafe.IsSensitivePath(provPath); reason != "" {
		checks = append(checks, PathCheck{
			Section: "Security", Name: "read guard", Status: PathPass,
			Detail:   "Sensitive paths blocked for Read tool",
			Blocking: true,
		})
	} else {
		checks = append(checks, PathCheck{
			Section: "Security", Name: "read guard", Status: PathFail,
			Detail:   "provider.json not blocked by Read tool",
			Blocking: true,
		})
	}

	pre := EnginePreflightReport(ctx)
	if pre.Ready {
		checks = append(checks, PathCheck{
			Section: "Ecosystem", Name: "flux", Status: PathPass,
			Detail:   "Preflight ready to chat",
			Blocking: true,
		})
	} else {
		status := PathWarn
		for _, c := range pre.Checks {
			if c.Status == gateway.CheckFail {
				status = PathFail
				break
			}
		}
		checks = append(checks, PathCheck{
			Section: "Ecosystem", Name: "flux", Status: status,
			Detail:   "Preflight not ready — see rho preflight",
			FixHint:  "Complete /config (credentials + model)",
			Blocking: true,
		})
	}

	sample := token.CountTokensFast("rho developer path readiness")
	checks = append(checks, PathCheck{
		Section: "Ecosystem", Name: "token", Status: PathPass,
		Detail: fmt.Sprintf("Embedded token/compress pipeline OK (sample=%d tokens)", sample),
	})

	report := DeveloperPathReport{Checks: checks}
	report.SecureReady = !anyBlockingFail(checks, "Security")
	report.ChatReady = setup.HasCredentials && setup.HasModel && pre.Ready
	report.Ready = report.ChatReady && report.SecureReady
	report.NextStep = developerPathNextStep(report, setup)
	return report
}

func anyBlockingFail(checks []PathCheck, section string) bool {
	for _, c := range checks {
		if c.Section == section && c.Blocking && c.Status == PathFail {
			return true
		}
	}
	return false
}

func developerPathNextStep(r DeveloperPathReport, setup SetupState) string {
	if r.Ready {
		return "Ready — run rho and start chatting"
	}
	if !setup.HasCredentials {
		return "Run rho → /config → paste API key (or Ollama local)"
	}
	if !setup.HasModel {
		return "Run /config → pick a model from the catalog"
	}
	if !r.SecureReady {
		return "Fix security items above (provider.json secrets, read guard)"
	}
	return "Run rho preflight for details, then /config if needed"
}

// pathStatusColor maps a readiness status to a semantic report color.
func pathStatusColor(s PathCheckStatus) color.Color {
	switch s {
	case PathPass:
		return theme.ReportSuccess
	case PathWarn:
		return theme.ReportWarn
	case PathFail:
		return theme.ReportError
	default:
		return theme.ReportMuted
	}
}

// FormatDeveloperPathReport renders the developer path readiness report for CLI/TUI.
func FormatDeveloperPathReport(ctx context.Context) string {
	r := EvaluateDeveloperPath(ctx)
	var b strings.Builder
	b.WriteString(theme.Tint("Developer path (rho · flux · token engine)", theme.ReportInfo) + "\n\n")

	status := "NEEDS SETUP"
	statusColor := theme.ReportWarn
	switch {
	case r.Ready:
		status = "READY"
		statusColor = theme.ReportSuccess
	case r.ChatReady && !r.SecureReady:
		status = "SECURITY FIX NEEDED"
		statusColor = theme.ReportError
	case r.SecureReady && !r.ChatReady:
		status = "ALMOST READY"
		statusColor = theme.ReportWarn
	}
	b.WriteString(theme.Tint("Status:", theme.ReportMuted) + " " + theme.Tint(status, statusColor) + "\n\n")

	sections := []string{"Setup", "Security", "Ecosystem"}
	for _, sec := range sections {
		b.WriteString(theme.Tint(sec, theme.ReportInfo) + "\n")
		for _, c := range r.Checks {
			if c.Section != sec {
				continue
			}
			b.WriteString(fmt.Sprintf("  %s %s — %s\n",
				theme.Tint(pathStatusGlyph(c.Status), pathStatusColor(c.Status)),
				theme.Tint(c.Name, theme.ReportMuted),
				theme.Tint(c.Detail, theme.ReportInfo)))
			if c.FixHint != "" && c.Status != PathPass {
				b.WriteString("      " + theme.Tint("→ "+c.FixHint, theme.ReportWarn) + "\n")
			}
		}
		b.WriteByte('\n')
	}

	b.WriteString(theme.Tint("Next:", theme.ReportMuted) + " " + r.NextStep + "\n")
	b.WriteString("\n" + theme.Tint("Docs: docs/DEVELOPER-PATH.md · docs/SECURITY-DEVELOPER.md · rho doctor · rho preflight", theme.ReportMuted) + "\n")
	return strings.TrimRight(b.String(), "\n")
}

func pathStatusGlyph(s PathCheckStatus) string {
	switch s {
	case PathPass:
		return icons.CheckBold()
	case PathWarn:
		return "!"
	case PathFail:
		return icons.CloseThick()
	default:
		return "?"
	}
}

func providerJSONHasSecretsOnDisk() (bool, string) {
	status := ProviderStateSecurityStatus()
	if status.Error != "" {
		return true, status.Error
	}
	return status.HasSecrets, status.Detail
}
