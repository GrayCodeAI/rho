package workspace

import "strings"

// GitOutputFunc is the narrow git boundary used by DiffReport.
type GitOutputFunc func(args ...string) (string, error)

// DiffReport returns the current working-tree diff, falling back to the index
// when there are no unstaged changes. An empty report means no changes exist.
func DiffReport() (string, error) {
	return DiffReportWith(GitOutput)
}

// DiffReportWith makes diff selection and truncation independently testable.
func DiffReportWith(git GitOutputFunc) (string, error) {
	if git == nil {
		return "", errGitRunnerRequired{}
	}
	stat, statErr := git("diff", "--stat")
	diff, diffErr := git("diff")
	if statErr != nil {
		return "", statErr
	}
	if diffErr != nil {
		return "", diffErr
	}
	if strings.TrimSpace(diff) == "" {
		stat, statErr = git("diff", "--cached", "--stat")
		diff, diffErr = git("diff", "--cached")
	}
	if statErr != nil {
		return "", statErr
	}
	if diffErr != nil {
		return "", diffErr
	}
	if strings.TrimSpace(diff) == "" {
		return "", nil
	}
	output := stat + "\n\n" + diff
	if len(output) > 10000 {
		return stat + "\n\n(diff too large, showing stat only)", nil
	}
	return output, nil
}

type errGitRunnerRequired struct{}

func (errGitRunnerRequired) Error() string { return "git runner is required" }
