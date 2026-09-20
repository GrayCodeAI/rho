package cmd

import (
	"context"

	reviewfeature "github.com/GrayCodeAI/rho/internal/features/review"
)

// Compatibility aliases keep existing command and workflow code stable while
// the review capability is owned by its feature package.
type (
	ReviewChatFn  = reviewfeature.ChatFunc
	ReviewConcern = reviewfeature.Concern
	ReviewFinding = reviewfeature.Finding
)

func DefaultConcerns() []ReviewConcern {
	return reviewfeature.DefaultConcerns()
}

func RunReviewPipeline(ctx context.Context, files []string, concerns []ReviewConcern, chatFn ReviewChatFn) ([]ReviewFinding, string) {
	return reviewfeature.Run(ctx, files, concerns, chatFn)
}

func FormatReviewReport(findings []ReviewFinding) string {
	return reviewfeature.FormatReport(findings)
}

func buildReviewPrompt(files []string, concern ReviewConcern) string {
	return reviewfeature.BuildPrompt(files, concern)
}

func parseReviewFindings(response, concernName string) []ReviewFinding {
	return reviewfeature.ParseFindings(response, concernName)
}

func reviewForConcern(ctx context.Context, files []string, concern ReviewConcern, chatFn ReviewChatFn) []ReviewFinding {
	return reviewfeature.RunConcern(ctx, files, concern, chatFn)
}

func deduplicateFindings(findings []ReviewFinding) []ReviewFinding {
	return reviewfeature.DeduplicateFindings(findings)
}

func sortBySeverity(findings []ReviewFinding) {
	reviewfeature.SortBySeverity(findings)
}
