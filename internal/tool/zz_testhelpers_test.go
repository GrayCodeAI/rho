package tool

import (
	"context"
	"path/filepath"
)

// testCtx returns a context carrying a permissive ToolContext so model-facing
// tools can be exercised without an engine. AllowedDirectories is the
// filesystem root so any temp path is permitted. Tests that assert path-boundary
// rejection must build their own restrictive ToolContext via WithToolContext.
func testCtx() context.Context {
	return WithToolContext(context.Background(), &ToolContext{
		AllowedDirectories: []string{string(filepath.Separator)},
	})
}
