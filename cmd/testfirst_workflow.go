package cmd

import executionfeature "github.com/GrayCodeAI/rho/internal/features/execution"

// Compatibility aliases keep callers stable while execution owns the
// test-first workflow implementation.
type TestFirstConfig = executionfeature.TestFirstConfig
type TestFirstResult = executionfeature.TestFirstResult

func DefaultTestFirstConfig() TestFirstConfig {
	return executionfeature.DefaultTestFirstConfig()
}

func RunTestFirstWorkflow(cfg TestFirstConfig, chatFn ReviewChatFn) TestFirstResult {
	return executionfeature.RunTestFirst(cfg, executionfeature.ChatFunc(chatFn))
}

func buildTestFixPrompt(testOutput string, round, maxRounds int) string {
	return executionfeature.BuildTestFixPrompt(testOutput, round, maxRounds)
}
