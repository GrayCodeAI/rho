package fingerprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectLanguages_Extensions(t *testing.T) {
	dir := t.TempDir()

	// Create files with different extensions.
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {}\n")
	writeTestFile(t, filepath.Join(dir, "util.go"), "package main\n\nfunc util() {}\n")
	writeTestFile(t, filepath.Join(dir, "handler.go"), "package main\n\nfunc handler() {}\n")
	writeTestFile(t, filepath.Join(dir, "app.ts"), "const x = 1;\n")
	writeTestFile(t, filepath.Join(dir, "style.css"), "body { color: red; }\n")

	langs := detectLanguages(dir)

	if len(langs) == 0 {
		t.Fatal("expected at least one language")
	}

	// Go should be first (3 files).
	if langs[0].Name != "Go" {
		t.Errorf("expected Go as primary language, got %q", langs[0].Name)
	}
	if langs[0].FileCount != 3 {
		t.Errorf("expected 3 Go files, got %d", langs[0].FileCount)
	}

	// Check percentage.
	expectedPct := 3.0 / 5.0 * 100
	if langs[0].Percentage < expectedPct-0.1 || langs[0].Percentage > expectedPct+0.1 {
		t.Errorf("expected ~%.1f%% for Go, got %.1f%%", expectedPct, langs[0].Percentage)
	}

	// Check that all languages are detected.
	langMap := make(map[string]int)
	for _, l := range langs {
		langMap[l.Name] = l.FileCount
	}
	if langMap["TypeScript"] != 1 {
		t.Errorf("expected 1 TypeScript file, got %d", langMap["TypeScript"])
	}
	if langMap["CSS"] != 1 {
		t.Errorf("expected 1 CSS file, got %d", langMap["CSS"])
	}
}

func TestDetectLanguages_SortedByFileCount(t *testing.T) {
	dir := t.TempDir()

	// Create 5 Python files, 3 JS files, 1 Go file.
	for i := 0; i < 5; i++ {
		writeTestFile(t, filepath.Join(dir, strings.Repeat("a", i+1)+".py"), "# python\n")
	}
	for i := 0; i < 3; i++ {
		writeTestFile(t, filepath.Join(dir, strings.Repeat("b", i+1)+".js"), "// js\n")
	}
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n")

	langs := detectLanguages(dir)

	if len(langs) < 3 {
		t.Fatalf("expected at least 3 languages, got %d", len(langs))
	}

	// Should be sorted: Python (5), JavaScript (3), Go (1).
	if langs[0].Name != "Python" {
		t.Errorf("expected Python first, got %q", langs[0].Name)
	}
	if langs[1].Name != "JavaScript" {
		t.Errorf("expected JavaScript second, got %q", langs[1].Name)
	}
	if langs[2].Name != "Go" {
		t.Errorf("expected Go third, got %q", langs[2].Name)
	}
}

func TestDetectFramework_GoMod(t *testing.T) {
	tests := []struct {
		name     string
		gomod    string
		expected string
	}{
		{
			name: "chi",
			gomod: `module example.com/app

go 1.21

require (
	github.com/go-chi/chi/v5 v5.0.10
)
`,
			expected: "chi",
		},
		{
			name: "gin",
			gomod: `module example.com/app

go 1.21

require (
	github.com/gin-gonic/gin v1.9.1
)
`,
			expected: "gin",
		},
		{
			name: "echo",
			gomod: `module example.com/app

go 1.21

require (
	github.com/labstack/echo/v4 v4.11.0
)
`,
			expected: "echo",
		},
		{
			name: "fiber",
			gomod: `module example.com/app

go 1.21

require (
	github.com/gofiber/fiber/v2 v2.50.0
)
`,
			expected: "fiber",
		},
		{
			name: "gorilla",
			gomod: `module example.com/app

go 1.21

require (
	github.com/gorilla/mux v1.8.0
)
`,
			expected: "gorilla",
		},
		{
			name:     "no framework",
			gomod:    "module example.com/app\n\ngo 1.21\n",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, "go.mod"), tt.gomod)

			result := detectFramework(dir, "Go")
			if result != tt.expected {
				t.Errorf("expected framework %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestDetectFramework_PackageJSON(t *testing.T) {
	tests := []struct {
		name     string
		pkg      string
		expected string
	}{
		{
			name:     "next.js",
			pkg:      `{"dependencies": {"next": "^13.0.0", "react": "^18.0.0"}}`,
			expected: "next.js",
		},
		{
			name:     "express",
			pkg:      `{"dependencies": {"express": "^4.18.0"}}`,
			expected: "express",
		},
		{
			name:     "vue",
			pkg:      `{"dependencies": {"vue": "^3.0.0"}}`,
			expected: "vue",
		},
		{
			name:     "angular",
			pkg:      `{"dependencies": {"@angular/core": "^16.0.0"}}`,
			expected: "angular",
		},
		{
			name:     "react only",
			pkg:      `{"dependencies": {"react": "^18.0.0"}}`,
			expected: "react",
		},
		{
			name:     "no framework",
			pkg:      `{"dependencies": {"lodash": "^4.0.0"}}`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, "package.json"), tt.pkg)

			result := detectFramework(dir, "JavaScript")
			if result != tt.expected {
				t.Errorf("expected framework %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestDetectFramework_Python(t *testing.T) {
	tests := []struct {
		name         string
		requirements string
		expected     string
	}{
		{
			name:         "django",
			requirements: "django==4.2\ncelery==5.3\n",
			expected:     "django",
		},
		{
			name:         "flask",
			requirements: "flask==2.3\n",
			expected:     "flask",
		},
		{
			name:         "fastapi",
			requirements: "fastapi==0.100.0\nuvicorn==0.23.0\n",
			expected:     "fastapi",
		},
		{
			name:         "no framework",
			requirements: "requests==2.31\nnumpy==1.25\n",
			expected:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, "requirements.txt"), tt.requirements)

			result := detectFramework(dir, "Python")
			if result != tt.expected {
				t.Errorf("expected framework %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestDetectFramework_Rust(t *testing.T) {
	tests := []struct {
		name     string
		cargo    string
		expected string
	}{
		{
			name: "actix",
			cargo: `[package]
name = "myapp"
version = "0.1.0"

[dependencies]
actix-web = "4"
`,
			expected: "actix",
		},
		{
			name: "axum",
			cargo: `[package]
name = "myapp"
version = "0.1.0"

[dependencies]
axum = "0.6"
tokio = { version = "1", features = ["full"] }
`,
			expected: "axum",
		},
		{
			name: "rocket",
			cargo: `[package]
name = "myapp"
version = "0.1.0"

[dependencies]
rocket = "0.5"
`,
			expected: "rocket",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, "Cargo.toml"), tt.cargo)

			result := detectFramework(dir, "Rust")
			if result != tt.expected {
				t.Errorf("expected framework %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestDetectCISystem(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(dir string)
		expected string
	}{
		{
			name: "github-actions",
			setup: func(dir string) {
				os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755)
			},
			expected: "github-actions",
		},
		{
			name: "gitlab-ci",
			setup: func(dir string) {
				writeTestFile2(filepath.Join(dir, ".gitlab-ci.yml"), "stages:\n  - test\n")
			},
			expected: "gitlab-ci",
		},
		{
			name: "circleci",
			setup: func(dir string) {
				os.MkdirAll(filepath.Join(dir, ".circleci"), 0o755)
			},
			expected: "circleci",
		},
		{
			name: "jenkins",
			setup: func(dir string) {
				writeTestFile2(filepath.Join(dir, "Jenkinsfile"), "pipeline {}\n")
			},
			expected: "jenkins",
		},
		{
			name: "travis-ci",
			setup: func(dir string) {
				writeTestFile2(filepath.Join(dir, ".travis.yml"), "language: go\n")
			},
			expected: "travis-ci",
		},
		{
			name:     "no CI",
			setup:    func(dir string) {},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)

			result := detectCISystem(dir)
			if result != tt.expected {
				t.Errorf("expected CI %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestDetectBuildSystem(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		content  string
		expected string
	}{
		{"go modules", "go.mod", "module example.com/app\n\ngo 1.21\n", "go modules"},
		{"npm", "package.json", `{"name": "app"}`, "npm"},
		{"cargo", "Cargo.toml", "[package]\nname = \"app\"\n", "cargo"},
		{"maven", "pom.xml", "<project></project>", "maven"},
		{"gradle", "build.gradle", "plugins { id 'java' }", "gradle"},
		{"cmake", "CMakeLists.txt", "cmake_minimum_required(VERSION 3.10)", "cmake"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTestFile(t, filepath.Join(dir, tt.file), tt.content)

			result := detectBuildSystem(dir)
			if result != tt.expected {
				t.Errorf("expected build system %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestDetectTestFramework(t *testing.T) {
	t.Run("go test", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.21\n")
		writeTestFile(t, filepath.Join(dir, "main_test.go"), "package main\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\n")

		result := detectTestFramework(dir, "Go")
		if result != "go test" {
			t.Errorf("expected 'go test', got %q", result)
		}
	})

	t.Run("go test + testify", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.21\n\nrequire (\n\tgithub.com/stretchr/testify v1.8.0\n)\n")
		writeTestFile(t, filepath.Join(dir, "main_test.go"), "package main\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) {}\n")

		result := detectTestFramework(dir, "Go")
		if result != "go test + testify" {
			t.Errorf("expected 'go test + testify', got %q", result)
		}
	})

	t.Run("jest", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "package.json"), `{"devDependencies": {"jest": "^29.0.0"}}`)

		result := detectTestFramework(dir, "JavaScript")
		if result != "jest" {
			t.Errorf("expected 'jest', got %q", result)
		}
	})

	t.Run("vitest", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "package.json"), `{"devDependencies": {"vitest": "^0.34.0"}}`)

		result := detectTestFramework(dir, "TypeScript")
		if result != "vitest" {
			t.Errorf("expected 'vitest', got %q", result)
		}
	})

	t.Run("pytest", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "requirements.txt"), "pytest==7.4.0\nflask==2.3.0\n")

		result := detectTestFramework(dir, "Python")
		if result != "pytest" {
			t.Errorf("expected 'pytest', got %q", result)
		}
	})

	t.Run("pytest from conftest", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "conftest.py"), "import pytest\n")

		result := detectTestFramework(dir, "Python")
		if result != "pytest" {
			t.Errorf("expected 'pytest', got %q", result)
		}
	})

	t.Run("cargo test", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "Cargo.toml"), "[package]\nname = \"app\"\nversion = \"0.1.0\"\n")

		result := detectTestFramework(dir, "Rust")
		if result != "cargo test" {
			t.Errorf("expected 'cargo test', got %q", result)
		}
	})

	t.Run("rspec", func(t *testing.T) {
		dir := t.TempDir()
		os.Mkdir(filepath.Join(dir, "spec"), 0o755)
		writeTestFile(t, filepath.Join(dir, "Gemfile"), "gem 'rails'\ngem 'rspec'\n")

		result := detectTestFramework(dir, "Ruby")
		if result != "rspec" {
			t.Errorf("expected 'rspec', got %q", result)
		}
	})
}

func TestDetectDocker(t *testing.T) {
	t.Run("with Dockerfile", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "Dockerfile"), "FROM golang:1.21\n")

		if !detectDocker(dir) {
			t.Error("expected Docker=true with Dockerfile")
		}
	})

	t.Run("with docker-compose", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "docker-compose.yml"), "version: '3'\n")

		if !detectDocker(dir) {
			t.Error("expected Docker=true with docker-compose.yml")
		}
	})

	t.Run("with compose.yaml", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "compose.yaml"), "services:\n")

		if !detectDocker(dir) {
			t.Error("expected Docker=true with compose.yaml")
		}
	})

	t.Run("no docker", func(t *testing.T) {
		dir := t.TempDir()

		if detectDocker(dir) {
			t.Error("expected Docker=false for empty dir")
		}
	})
}

func TestDetectMonorepo_MultipleGoMod(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "pkg1"), 0o755)
	os.MkdirAll(filepath.Join(dir, "pkg2"), 0o755)
	writeTestFile(t, filepath.Join(dir, "pkg1", "go.mod"), "module example.com/pkg1\n")
	writeTestFile(t, filepath.Join(dir, "pkg2", "go.mod"), "module example.com/pkg2\n")

	if !detectMonorepo(dir) {
		t.Error("expected Monorepo=true with multiple go.mod files")
	}
}

func TestDetectMonorepo_GoWork(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.work"), "go 1.21\n\nuse (\n\t./pkg1\n\t./pkg2\n)\n")

	if !detectMonorepo(dir) {
		t.Error("expected Monorepo=true with go.work")
	}
}

func TestDetectMonorepo_PackagesDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "packages"), 0o755)

	if !detectMonorepo(dir) {
		t.Error("expected Monorepo=true with packages/ directory")
	}
}

func TestDetectMonorepo_Workspaces(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "package.json"), `{"name": "monorepo", "workspaces": ["packages/*"]}`)

	if !detectMonorepo(dir) {
		t.Error("expected Monorepo=true with workspaces in package.json")
	}
}

func TestDetectMonorepo_LernaJSON(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "lerna.json"), `{"version": "1.0.0"}`)

	if !detectMonorepo(dir) {
		t.Error("expected Monorepo=true with lerna.json")
	}
}

func TestDetectMonorepo_NotMonorepo(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), "package main\n")

	if detectMonorepo(dir) {
		t.Error("expected Monorepo=false for simple project")
	}
}
