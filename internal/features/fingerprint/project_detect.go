package fingerprint

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// This file holds the language/framework/build/CI/etc. detectors used by Scan.
// Convention detection lives in project_conventions.go; the Scan orchestration,
// types, recommendations, and summary formatting live in project.go.

// detectLanguages walks the project directory, maps extensions to languages,
// calculates percentages, and returns results sorted by file count descending.
func detectLanguages(dir string) []ProjectLangInfo {
	counts := make(map[string]int)

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}

		ext := filepath.Ext(path)
		if lang, ok := extToLang[ext]; ok {
			counts[lang]++
		}
		return nil
	})

	if len(counts) == 0 {
		return nil
	}

	total := 0
	for _, c := range counts {
		total += c
	}

	langs := make([]ProjectLangInfo, 0, len(counts))
	for name, count := range counts {
		pct := float64(count) / float64(total) * 100
		langs = append(langs, ProjectLangInfo{
			Name:       name,
			FileCount:  count,
			Percentage: pct,
		})
	}

	sort.Slice(langs, func(i, j int) bool {
		return langs[i].FileCount > langs[j].FileCount
	})

	return langs
}

// detectFramework reads key config files to determine the web/app framework.
func detectFramework(dir string, primaryLang string) string {
	switch primaryLang {
	case "Go":
		return detectGoFramework(dir)
	case "Python":
		return detectPythonFramework(dir)
	case "JavaScript", "TypeScript":
		return detectJSFramework(dir)
	case "Rust":
		return detectRustFramework(dir)
	}
	return ""
}

// detectGoFramework reads go.mod for known Go web frameworks.
func detectGoFramework(dir string) string {
	goModPath := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(goModPath) // #nosec G304 -- goModPath joins a fixed manifest filename with a project directory being scanned by this dev tool
	if err != nil {
		return ""
	}
	content := string(data)

	frameworks := []struct {
		module string
		name   string
	}{
		{"github.com/go-chi/chi", "chi"},
		{"github.com/gin-gonic/gin", "gin"},
		{"github.com/labstack/echo", "echo"},
		{"github.com/gofiber/fiber", "fiber"},
		{"github.com/gorilla/mux", "gorilla"},
		{"github.com/julienschmidt/httprouter", "httprouter"},
		{"github.com/valyala/fasthttp", "fasthttp"},
	}

	for _, fw := range frameworks {
		if strings.Contains(content, fw.module) {
			return fw.name
		}
	}

	// Check for net/http usage (fallback — it's in stdlib so not in go.mod).
	return ""
}

// detectPythonFramework reads requirements.txt, Pipfile, or pyproject.toml.
func detectPythonFramework(dir string) string {
	files := []string{
		filepath.Join(dir, "requirements.txt"),
		filepath.Join(dir, "Pipfile"),
		filepath.Join(dir, "pyproject.toml"),
		filepath.Join(dir, "setup.py"),
	}

	frameworks := []struct {
		keyword string
		name    string
	}{
		{"django", "django"},
		{"Django", "django"},
		{"flask", "flask"},
		{"Flask", "flask"},
		{"fastapi", "fastapi"},
		{"FastAPI", "fastapi"},
		{"tornado", "tornado"},
		{"starlette", "starlette"},
		{"sanic", "sanic"},
	}

	for _, f := range files {
		data, err := os.ReadFile(f) // #nosec G304 -- f is a fixed manifest filename joined with a project directory being scanned by this dev tool
		if err != nil {
			continue
		}
		content := string(data)
		for _, fw := range frameworks {
			if strings.Contains(content, fw.keyword) {
				return fw.name
			}
		}
	}

	return ""
}

// detectJSFramework reads package.json for known JS/TS frameworks.
func detectJSFramework(dir string) string {
	pkgPath := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(pkgPath) // #nosec G304 -- pkgPath joins a fixed manifest filename with a project directory being scanned by this dev tool
	if err != nil {
		return ""
	}

	var pkg struct {
		Dependencies    map[string]interface{} `json:"dependencies"`
		DevDependencies map[string]interface{} `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}

	// Merge deps for lookup.
	allDeps := make(map[string]bool)
	for k := range pkg.Dependencies {
		allDeps[k] = true
	}
	for k := range pkg.DevDependencies {
		allDeps[k] = true
	}

	frameworks := []struct {
		pkg  string
		name string
	}{
		{"next", "next.js"},
		{"nuxt", "nuxt"},
		{"@angular/core", "angular"},
		{"vue", "vue"},
		{"svelte", "svelte"},
		{"express", "express"},
		{"fastify", "fastify"},
		{"koa", "koa"},
		{"hapi", "hapi"},
		{"react", "react"},
		{"gatsby", "gatsby"},
		{"remix", "remix"},
	}

	for _, fw := range frameworks {
		if allDeps[fw.pkg] {
			return fw.name
		}
	}

	return ""
}

// detectRustFramework reads Cargo.toml for known Rust web frameworks.
func detectRustFramework(dir string) string {
	cargoPath := filepath.Join(dir, "Cargo.toml")
	data, err := os.ReadFile(cargoPath) // #nosec G304 -- cargoPath joins a fixed manifest filename with a project directory being scanned by this dev tool
	if err != nil {
		return ""
	}
	content := string(data)

	frameworks := []struct {
		crate string
		name  string
	}{
		{"actix-web", "actix"},
		{"rocket", "rocket"},
		{"axum", "axum"},
		{"warp", "warp"},
		{"tide", "tide"},
	}

	for _, fw := range frameworks {
		if strings.Contains(content, fw.crate) {
			return fw.name
		}
	}

	return ""
}

// detectBuildSystem determines the project's build system from manifest files.
func detectBuildSystem(dir string) string {
	buildSystems := []struct {
		file   string
		system string
	}{
		{"go.mod", "go modules"},
		{"package.json", "npm"},
		{"Cargo.toml", "cargo"},
		{"pom.xml", "maven"},
		{"build.gradle", "gradle"},
		{"build.gradle.kts", "gradle"},
		{"CMakeLists.txt", "cmake"},
		{"Makefile", "make"},
		{"meson.build", "meson"},
		{"BUILD", "bazel"},
		{"WORKSPACE", "bazel"},
		{"mix.exs", "mix"},
		{"pubspec.yaml", "pub"},
		{"Package.swift", "swift package manager"},
		{"Rakefile", "rake"},
	}

	for _, bs := range buildSystems {
		path := filepath.Join(dir, bs.file)
		if _, err := os.Stat(path); err == nil {
			return bs.system
		}
	}

	return ""
}

// detectProjectPackageManager determines the project's package manager.
func detectProjectPackageManager(dir string) string {
	managers := []struct {
		file    string
		manager string
	}{
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"package-lock.json", "npm"},
		{"bun.lockb", "bun"},
		{"go.sum", "go modules"},
		{"Cargo.lock", "cargo"},
		{"Pipfile.lock", "pipenv"},
		{"poetry.lock", "poetry"},
		{"Gemfile.lock", "bundler"},
		{"composer.lock", "composer"},
		{"pubspec.lock", "pub"},
		{"mix.lock", "mix"},
	}

	for _, m := range managers {
		path := filepath.Join(dir, m.file)
		if _, err := os.Stat(path); err == nil {
			return m.manager
		}
	}

	// Fallback: check manifest files.
	fallbacks := []struct {
		file    string
		manager string
	}{
		{"go.mod", "go modules"},
		{"package.json", "npm"},
		{"Cargo.toml", "cargo"},
		{"requirements.txt", "pip"},
		{"pyproject.toml", "pip"},
		{"Gemfile", "bundler"},
		{"composer.json", "composer"},
	}

	for _, f := range fallbacks {
		path := filepath.Join(dir, f.file)
		if _, err := os.Stat(path); err == nil {
			return f.manager
		}
	}

	return ""
}

// detectTestFramework determines the test framework used.
func detectTestFramework(dir string, lang string) string {
	switch lang {
	case "Go":
		// Go has a built-in test framework.
		// Check for testify or other test libs in go.mod.
		goModPath := filepath.Join(dir, "go.mod")
		// #nosec G304 -- goModPath joins a fixed manifest filename with a project directory being scanned by this dev tool
		if data, err := os.ReadFile(goModPath); err == nil {
			content := string(data)
			if strings.Contains(content, "github.com/stretchr/testify") {
				return "go test + testify"
			}
		}
		// Check if there are any _test.go files.
		hasTests := false
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || hasTests {
				return filepath.SkipAll
			}
			if d.IsDir() && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), "_test.go") {
				hasTests = true
				return filepath.SkipAll
			}
			return nil
		})
		if hasTests {
			return "go test"
		}
		return ""

	case "JavaScript", "TypeScript":
		pkgPath := filepath.Join(dir, "package.json")
		data, err := os.ReadFile(pkgPath) // #nosec G304 -- pkgPath joins a fixed manifest filename with a project directory being scanned by this dev tool
		if err != nil {
			return ""
		}
		var pkg struct {
			Dependencies    map[string]interface{} `json:"dependencies"`
			DevDependencies map[string]interface{} `json:"devDependencies"`
			Scripts         map[string]string      `json:"scripts"`
		}
		if err := json.Unmarshal(data, &pkg); err != nil {
			return ""
		}

		allDeps := make(map[string]bool)
		for k := range pkg.Dependencies {
			allDeps[k] = true
		}
		for k := range pkg.DevDependencies {
			allDeps[k] = true
		}

		if allDeps["vitest"] {
			return "vitest"
		}
		if allDeps["jest"] || allDeps["@jest/core"] || allDeps["ts-jest"] {
			return "jest"
		}
		if allDeps["mocha"] {
			return "mocha"
		}
		if allDeps["ava"] {
			return "ava"
		}
		if allDeps["cypress"] {
			return "cypress"
		}
		if allDeps["playwright"] || allDeps["@playwright/test"] {
			return "playwright"
		}
		return ""

	case "Python":
		// Check for pytest in requirements or installed.
		files := []string{
			filepath.Join(dir, "requirements.txt"),
			filepath.Join(dir, "requirements-dev.txt"),
			filepath.Join(dir, "pyproject.toml"),
			filepath.Join(dir, "setup.cfg"),
			filepath.Join(dir, "Pipfile"),
		}
		for _, f := range files {
			data, err := os.ReadFile(f) // #nosec G304 -- f is a fixed manifest filename joined with a project directory being scanned by this dev tool
			if err != nil {
				continue
			}
			content := string(data)
			if strings.Contains(content, "pytest") {
				return "pytest"
			}
		}
		// Check for pytest.ini or conftest.py.
		if _, err := os.Stat(filepath.Join(dir, "pytest.ini")); err == nil {
			return "pytest"
		}
		if _, err := os.Stat(filepath.Join(dir, "conftest.py")); err == nil {
			return "pytest"
		}
		// Check for test files with unittest patterns.
		hasUnittest := false
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || hasUnittest {
				return filepath.SkipAll
			}
			if d.IsDir() && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			if !d.IsDir() && (strings.HasPrefix(d.Name(), "test_") || strings.HasSuffix(d.Name(), "_test.py")) {
				hasUnittest = true
				return filepath.SkipAll
			}
			return nil
		})
		if hasUnittest {
			return "unittest"
		}
		return ""

	case "Rust":
		// Rust uses built-in test framework via cargo test.
		cargoPath := filepath.Join(dir, "Cargo.toml")
		if _, err := os.Stat(cargoPath); err == nil {
			return "cargo test"
		}
		return ""

	case "Java":
		pomPath := filepath.Join(dir, "pom.xml")
		// #nosec G304 -- pomPath joins a fixed manifest filename with a project directory being scanned by this dev tool
		if data, err := os.ReadFile(pomPath); err == nil {
			content := string(data)
			if strings.Contains(content, "junit") || strings.Contains(content, "JUnit") {
				return "junit"
			}
			if strings.Contains(content, "testng") {
				return "testng"
			}
		}
		gradlePath := filepath.Join(dir, "build.gradle")
		// #nosec G304 -- gradlePath joins a fixed manifest filename with a project directory being scanned by this dev tool
		if data, err := os.ReadFile(gradlePath); err == nil {
			content := string(data)
			if strings.Contains(content, "junit") || strings.Contains(content, "JUnit") {
				return "junit"
			}
			if strings.Contains(content, "testng") {
				return "testng"
			}
		}
		return ""

	case "Ruby":
		if _, err := os.Stat(filepath.Join(dir, "spec")); err == nil {
			return "rspec"
		}
		gemPath := filepath.Join(dir, "Gemfile")
		// #nosec G304 -- gemPath joins a fixed manifest filename with a project directory being scanned by this dev tool
		if data, err := os.ReadFile(gemPath); err == nil {
			content := string(data)
			if strings.Contains(content, "rspec") {
				return "rspec"
			}
			if strings.Contains(content, "minitest") {
				return "minitest"
			}
		}
		return ""
	}

	return ""
}

// detectLintTools detects configured linting tools from config files.
func detectLintTools(dir string) []string {
	var tools []string

	lintConfigs := []struct {
		file string
		tool string
	}{
		{".golangci.yml", "golangci-lint"},
		{".golangci.yaml", "golangci-lint"},
		{".golangci.toml", "golangci-lint"},
		{".eslintrc", "eslint"},
		{".eslintrc.js", "eslint"},
		{".eslintrc.json", "eslint"},
		{".eslintrc.yml", "eslint"},
		{"eslint.config.js", "eslint"},
		{"eslint.config.mjs", "eslint"},
		{".prettierrc", "prettier"},
		{".prettierrc.js", "prettier"},
		{".prettierrc.json", "prettier"},
		{"prettier.config.js", "prettier"},
		{".stylelintrc", "stylelint"},
		{".stylelintrc.json", "stylelint"},
		{"stylelint.config.js", "stylelint"},
		{".flake8", "flake8"},
		{"setup.cfg", "flake8"}, // might contain flake8 config
		{".pylintrc", "pylint"},
		{"pyproject.toml", "ruff"}, // might contain ruff config
		{".rubocop.yml", "rubocop"},
		{"clippy.toml", "clippy"},
		{".clippy.toml", "clippy"},
		{".editorconfig", "editorconfig"},
		{"biome.json", "biome"},
		{"deno.json", "deno lint"},
		{".hadolint.yaml", "hadolint"},
		{".shellcheckrc", "shellcheck"},
	}

	seen := make(map[string]bool)
	for _, lc := range lintConfigs {
		path := filepath.Join(dir, lc.file)
		if _, err := os.Stat(path); err == nil {
			// Special case: setup.cfg / pyproject.toml may or may not contain lint config.
			if lc.file == "setup.cfg" {
				// #nosec G304 -- path joins a fixed config filename with a project directory being scanned by this dev tool
				if data, err := os.ReadFile(path); err == nil {
					if !strings.Contains(string(data), "[flake8]") {
						continue
					}
				}
			}
			if lc.file == "pyproject.toml" {
				// #nosec G304 -- path joins a fixed config filename with a project directory being scanned by this dev tool
				if data, err := os.ReadFile(path); err == nil {
					if !strings.Contains(string(data), "[tool.ruff]") && !strings.Contains(string(data), "ruff") {
						continue
					}
				}
			}
			if !seen[lc.tool] {
				seen[lc.tool] = true
				tools = append(tools, lc.tool)
			}
		}
	}

	// Check package.json for lint-related devDependencies.
	pkgPath := filepath.Join(dir, "package.json")
	// #nosec G304 -- pkgPath joins a fixed manifest filename with a project directory being scanned by this dev tool
	if data, err := os.ReadFile(pkgPath); err == nil {
		var pkg struct {
			DevDependencies map[string]interface{} `json:"devDependencies"`
		}
		if err := json.Unmarshal(data, &pkg); err == nil {
			jsDeps := []struct {
				pkg  string
				tool string
			}{
				{"eslint", "eslint"},
				{"prettier", "prettier"},
				{"stylelint", "stylelint"},
				{"biome", "biome"},
				{"@biomejs/biome", "biome"},
			}
			for _, jd := range jsDeps {
				if _, ok := pkg.DevDependencies[jd.pkg]; ok && !seen[jd.tool] {
					seen[jd.tool] = true
					tools = append(tools, jd.tool)
				}
			}
		}
	}

	return tools
}

// detectCISystem identifies the CI/CD system in use and returns its name.
func detectCISystem(dir string) string {
	ciSystems := []struct {
		path   string
		isDir  bool
		system string
	}{
		{filepath.Join(".github", "workflows"), true, "github-actions"},
		{".gitlab-ci.yml", false, "gitlab-ci"},
		{".circleci", true, "circleci"},
		{"Jenkinsfile", false, "jenkins"},
		{".travis.yml", false, "travis-ci"},
		{"azure-pipelines.yml", false, "azure-pipelines"},
		{"bitbucket-pipelines.yml", false, "bitbucket-pipelines"},
		{".drone.yml", false, "drone"},
		{".buildkite", true, "buildkite"},
		{"cloudbuild.yaml", false, "google-cloud-build"},
		{"cloudbuild.yml", false, "google-cloud-build"},
		{".tekton", true, "tekton"},
	}

	for _, ci := range ciSystems {
		full := filepath.Join(dir, ci.path)
		info, err := os.Stat(full)
		if err != nil {
			continue
		}
		if ci.isDir && info.IsDir() {
			return ci.system
		}
		if !ci.isDir && !info.IsDir() {
			return ci.system
		}
	}

	return ""
}

// detectDocker checks for Dockerfile or docker-compose files.
func detectDocker(dir string) bool {
	dockerFiles := []string{
		"Dockerfile",
		"dockerfile",
		"docker-compose.yml",
		"docker-compose.yaml",
		"compose.yml",
		"compose.yaml",
		".dockerignore",
	}

	for _, f := range dockerFiles {
		path := filepath.Join(dir, f)
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}

	return false
}

// detectMonorepo checks for indicators of a monorepo structure.
func detectMonorepo(dir string) bool {
	// Check for multiple go.mod files.
	goModCount := 0
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "go.mod" {
			goModCount++
			if goModCount > 1 {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if goModCount > 1 {
		return true
	}

	// Check for go.work file.
	if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
		return true
	}

	// Check for packages/ or apps/ directory (JS monorepos).
	monoDirs := []string{"packages", "apps", "modules", "services", "libs"}
	for _, md := range monoDirs {
		path := filepath.Join(dir, md)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return true
		}
	}

	// Check for lerna.json, pnpm-workspace.yaml, or turbo.json.
	monoFiles := []string{"lerna.json", "pnpm-workspace.yaml", "turbo.json", "nx.json"}
	for _, mf := range monoFiles {
		path := filepath.Join(dir, mf)
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}

	// Check package.json for workspaces field.
	pkgPath := filepath.Join(dir, "package.json")
	// #nosec G304 -- pkgPath joins a fixed manifest filename with a project directory being scanned by this dev tool
	if data, err := os.ReadFile(pkgPath); err == nil {
		var pkg map[string]interface{}
		if err := json.Unmarshal(data, &pkg); err == nil {
			if _, ok := pkg["workspaces"]; ok {
				return true
			}
		}
	}

	return false
}

// classifyProjectSize returns a size classification based on file count.
func classifyProjectSize(fileCount int) string {
	switch {
	case fileCount < 10:
		return "tiny"
	case fileCount < 100:
		return "small"
	case fileCount <= 1000:
		return "medium"
	default:
		return "large"
	}
}
