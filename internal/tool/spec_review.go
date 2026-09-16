package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/GrayCodeAI/rho/internal/spec"
)

type SpecReviewTool struct{}

func (SpecReviewTool) Name() string { return "SpecReview" }

// SpecReviewInput is the typed input for SpecReviewTool.
type SpecReviewInput struct {
	Scope string `json:"scope"`
}

func (SpecReviewTool) Aliases() []string {
	return []string{"spec_review", "spec:review"}
}

func (SpecReviewTool) Description() string {
	return "Post-implementation review against specs."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SpecReviewTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"scope": {Type: "string", Enum: []interface{}{"spec", "diff", "full"}, Description: "Review scope: spec, diff, full"},
		},
	}
}

func (SpecReviewTool) Parameters() map[string]interface{} {
	return specReviewSchema.ToJSONSchema()
}

// specReviewSchema is the single source of truth for SpecReview's input schema.
var specReviewSchema = SpecReviewTool{}.Schema()

func (SpecReviewTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SpecReviewInput]("SpecReview", input)
	if err != nil {
		return "", err
	}
	if p.Scope == "" {
		p.Scope = "spec"
	}

	dir, err := specDir(ctx)
	if err != nil {
		return "", err
	}

	if p.Scope == "spec" {
		return reviewAgainstSpec(dir)
	} else if p.Scope == "diff" {
		return reviewDiff(dir)
	}
	return reviewAgainstSpec(dir)
}

func reviewAgainstSpec(dir string) (string, error) {
	specContent := readFileStr(filepath.Join(dir, "spec.md"))
	if specContent == "" {
		return "No spec.md found.", nil
	}

	reqs := spec.ExtractReqIDs(specContent)

	var b strings.Builder
	b.WriteString("## Spec Compliance Review\n\n")
	fmt.Fprintf(&b, "**%d requirements** to verify\n\n", len(reqs))

	for _, req := range reqs {
		fmt.Fprintf(&b, "- `%s`\n", req.Raw)
	}

	return strings.TrimSpace(b.String()), nil
}

func reviewDiff(dir string) (string, error) {
	cmd := exec.Command("git", "diff", "--stat")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("git diff failed: %v", err), nil
	}

	var b strings.Builder
	b.WriteString("## Diff Review\n\n")
	b.WriteString("```\n")
	b.Write(output)
	b.WriteString("\n```\n")

	return strings.TrimSpace(b.String()), nil
}

func init() {
	_ = SpecReviewTool{}
}
