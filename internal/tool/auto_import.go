package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// AutoImporter resolves missing imports in Go source code by matching
// package-qualified symbols against a database of known packages.
type AutoImporter struct {
	KnownPackages map[string]string // symbol (package name) → import path
	mu            sync.RWMutex
}

// ImportFix describes a single missing import that should be added.
type ImportFix struct {
	File    string // file path where the fix applies
	Package string // package name (e.g., "fmt")
	Path    string // import path (e.g., "fmt" or "encoding/json")
	Symbol  string // the full qualified symbol reference (e.g., "fmt.Println")
	Line    int    // approximate line number where the symbol is used
}

// NewAutoImporter creates a new AutoImporter pre-loaded with 200+ common Go packages.
func NewAutoImporter() *AutoImporter {
	ai := &AutoImporter{
		KnownPackages: make(map[string]string, 256),
	}

	// Standard library packages
	stdPackages := map[string]string{
		// Core
		"fmt":     "fmt",
		"os":      "os",
		"io":      "io",
		"strings": "strings",
		"strconv": "strconv",
		"time":    "time",
		"sync":    "sync",
		"context": "context",
		"errors":  "errors",
		"log":     "log",
		"math":    "math",
		"sort":    "sort",
		"bytes":   "bytes",
		"bufio":   "bufio",
		"regexp":  "regexp",
		"reflect": "reflect",
		"runtime": "runtime",
		"unicode": "unicode",
		"unsafe":  "unsafe",

		// IO
		"ioutil": "io/ioutil",
		"fs":     "io/fs",

		// Encoding
		"json":   "encoding/json",
		"xml":    "encoding/xml",
		"csv":    "encoding/csv",
		"base64": "encoding/base64",
		"hex":    "encoding/hex",
		"binary": "encoding/binary",
		"gob":    "encoding/gob",
		"pem":    "encoding/pem",
		"asn1":   "encoding/asn1",

		// Crypto
		"sha256":   "crypto/sha256",
		"sha512":   "crypto/sha512",
		"md5":      "crypto/md5",
		"aes":      "crypto/aes",
		"cipher":   "crypto/cipher",
		"rand":     "crypto/rand",
		"rsa":      "crypto/rsa",
		"ecdsa":    "crypto/ecdsa",
		"tls":      "crypto/tls",
		"x509":     "crypto/x509",
		"hmac":     "crypto/hmac",
		"elliptic": "crypto/elliptic",
		"ed25519":  "crypto/ed25519",

		// Net
		"http":      "net/http",
		"url":       "net/url",
		"net":       "net",
		"smtp":      "net/smtp",
		"textproto": "net/textproto",
		"httputil":  "net/http/httputil",
		"httptest":  "net/http/httptest",
		"rpc":       "net/rpc",

		// OS / Path
		"filepath": "path/filepath",
		"path":     "path",
		"exec":     "os/exec",
		"signal":   "os/signal",
		"user":     "os/user",

		// Text
		"template":  "text/template",
		"tabwriter": "text/tabwriter",
		"scanner":   "text/scanner",

		// HTML
		"html": "html",

		// Go tooling
		"ast":      "go/ast",
		"parser":   "go/parser",
		"token":    "go/token",
		"printer":  "go/printer",
		"format":   "go/format",
		"types":    "go/types",
		"build":    "go/build",
		"constant": "go/constant",
		"importer": "go/importer",
		"doc":      "go/doc",

		// Database
		"sql": "database/sql",

		// Archive / Compress
		"tar":   "archive/tar",
		"zip":   "archive/zip",
		"gzip":  "compress/gzip",
		"flate": "compress/flate",
		"zlib":  "compress/zlib",
		"bzip2": "compress/bzip2",
		"lzw":   "compress/lzw",

		// Container
		"heap": "container/heap",
		"list": "container/list",
		"ring": "container/ring",

		// Sync extensions
		"atomic": "sync/atomic",

		// Math extensions
		"big":  "math/big",
		"bits": "math/bits",

		// Testing
		"testing": "testing",
		"iotest":  "testing/iotest",
		"quick":   "testing/quick",
		"fstest":  "testing/fstest",

		// Debug
		"elf":   "debug/elf",
		"dwarf": "debug/dwarf",
		"pe":    "debug/pe",

		// Embed
		"embed": "embed",

		// Expvar
		"expvar": "expvar",

		// Flag
		"flag": "flag",

		// Image
		"image":    "image",
		"imgcolor": "image/color",
		"draw":     "image/draw",
		"png":      "image/png",
		"jpeg":     "image/jpeg",
		"gif":      "image/gif",

		// Index
		"suffixarray": "index/suffixarray",

		// Mime
		"mime":      "mime",
		"multipart": "mime/multipart",

		// Plugin
		"plugin": "plugin",

		// Syslog
		"syslog": "log/syslog",

		// Strings/bytes extensions
		"utf8":  "unicode/utf8",
		"utf16": "unicode/utf16",

		// Hash
		"crc32":   "hash/crc32",
		"crc64":   "hash/crc64",
		"fnv":     "hash/fnv",
		"adler32": "hash/adler32",
		"hash":    "hash",
		"maphash": "hash/maphash",

		// Syscall
		"syscall": "syscall",

		// Slog (Go 1.21+)
		"slog": "log/slog",

		// Additional stdlib for broader coverage
		"pprof":     "net/http/pprof",
		"cookiejar": "net/http/cookiejar",
		"fcgi":      "net/http/fcgi",
		"cgi":       "net/http/cgi",
		"jsonrpc":   "net/rpc/jsonrpc",
		"macho":     "debug/macho",
		"plan9obj":  "debug/plan9obj",
		"buildinfo": "debug/buildinfo",
		"gosym":     "debug/gosym",
		"metrics":   "runtime/metrics",
		"maps":      "maps",
		"slices":    "slices",
		"cmp":       "cmp",
		"subtle":    "crypto/subtle",
		"dsa":       "crypto/dsa",
		"rc4":       "crypto/rc4",
		"des":       "crypto/des",
		"ecdh":      "crypto/ecdh",
	}

	// Popular third-party packages
	thirdParty := map[string]string{
		// Web frameworks
		"chi":        "github.com/go-chi/chi/v5",
		"gin":        "github.com/gin-gonic/gin",
		"echo":       "github.com/labstack/echo/v4",
		"fiber":      "github.com/gofiber/fiber/v2",
		"mux":        "github.com/gorilla/mux",
		"httprouter": "github.com/julienschmidt/httprouter",

		// CLI
		"cobra": "github.com/spf13/cobra",
		"viper": "github.com/spf13/viper",
		"pflag": "github.com/spf13/pflag",
		"cli":   "github.com/urfave/cli/v2",
		"color": "github.com/fatih/color",

		// Logging
		"zap":     "go.uber.org/zap",
		"zapcore": "go.uber.org/zap/zapcore",
		"logrus":  "github.com/sirupsen/logrus",
		"zerolog": "github.com/rs/zerolog",

		// Testing
		"assert":  "github.com/stretchr/testify/assert",
		"require": "github.com/stretchr/testify/require",
		"mock":    "github.com/stretchr/testify/mock",
		"suite":   "github.com/stretchr/testify/suite",
		"gomock":  "go.uber.org/mock/gomock",
		"gomega":  "github.com/onsi/gomega",
		"ginkgo":  "github.com/onsi/ginkgo/v2",

		// Database
		"sqlx":    "github.com/jmoiron/sqlx",
		"gorm":    "gorm.io/gorm",
		"pgx":     "github.com/jackc/pgx/v5",
		"sqlite3": "github.com/mattn/go-sqlite3",
		"redis":   "github.com/redis/go-redis/v9",
		"mongo":   "go.mongodb.org/mongo-driver/mongo",
		"bson":    "go.mongodb.org/mongo-driver/bson",

		// Serialization
		"yaml":     "gopkg.in/yaml.v3",
		"toml":     "github.com/BurntSushi/toml",
		"protobuf": "google.golang.org/protobuf/proto",
		"proto":    "google.golang.org/protobuf/proto",
		"msgpack":  "github.com/vmihailenco/msgpack/v5",

		// HTTP clients
		"resty":     "github.com/go-resty/resty/v2",
		"websocket": "github.com/gorilla/websocket",

		// Auth
		"jwt": "github.com/golang-jwt/jwt/v5",

		// UUID
		"uuid": "github.com/google/uuid",

		// Validation
		"validator": "github.com/go-playground/validator/v10",

		// Concurrency
		"errgroup":  "golang.org/x/sync/errgroup",
		"semaphore": "golang.org/x/sync/semaphore",

		// Observability
		"prometheus": "github.com/prometheus/client_golang/prometheus",
		"otel":       "go.opentelemetry.io/otel",
		"trace":      "go.opentelemetry.io/otel/trace",
		"metric":     "go.opentelemetry.io/otel/metric",

		// gRPC
		"grpc":   "google.golang.org/grpc",
		"codes":  "google.golang.org/grpc/codes",
		"status": "google.golang.org/grpc/status",

		// Cloud
		"aws": "github.com/aws/aws-sdk-go-v2/aws",
		"s3":  "github.com/aws/aws-sdk-go-v2/service/s3",

		// Templating
		"sprig": "github.com/Masterminds/sprig/v3",

		// Crypto extensions
		"bcrypt": "golang.org/x/crypto/bcrypt",
		"argon2": "golang.org/x/crypto/argon2",
		"ssh":    "golang.org/x/crypto/ssh",

		// Data structures
		"set": "github.com/deckarep/golang-set/v2",

		// Configuration
		"envconfig": "github.com/kelseyhightower/envconfig",
		"godotenv":  "github.com/joho/godotenv",

		// Migration
		"migrate": "github.com/golang-migrate/migrate/v4",

		// Swagger / OpenAPI
		"swag": "github.com/swaggo/swag",

		// Error handling
		"multierr": "go.uber.org/multierr",

		// Rate limiting
		"rate": "golang.org/x/time/rate",

		// Text processing extensions
		"diff": "github.com/sergi/go-diff/diffmatchpatch",

		// Additional popular third-party
		"afero":       "github.com/spf13/afero",
		"cast":        "github.com/spf13/cast",
		"testify":     "github.com/stretchr/testify",
		"decimal":     "github.com/shopspring/decimal",
		"copier":      "github.com/jinzhu/copier",
		"cron":        "github.com/robfig/cron/v3",
		"squirrel":    "github.com/Masterminds/squirrel",
		"backoff":     "github.com/cenkalti/backoff/v4",
		"sarama":      "github.com/IBM/sarama",
		"nats":        "github.com/nats-io/nats.go",
		"amqp":        "github.com/rabbitmq/amqp091-go",
		"elastic":     "github.com/olivere/elastic/v7",
		"goquery":     "github.com/PuerkitoBio/goquery",
		"colly":       "github.com/gocolly/colly/v2",
		"chromedp":    "github.com/chromedp/chromedp",
		"excelize":    "github.com/xuri/excelize/v2",
		"imaging":     "github.com/disintegration/imaging",
		"resize":      "github.com/nfnt/resize",
		"bluemonday":  "github.com/microcosm-cc/bluemonday",
		"blackfriday": "github.com/russross/blackfriday/v2",
		"goldmark":    "github.com/yuin/goldmark",
		"casbin":      "github.com/casbin/casbin/v2",
		"wire":        "github.com/google/wire",
		"fx":          "go.uber.org/fx",
		"dig":         "go.uber.org/dig",
		"machinery":   "github.com/RichardKnop/machinery/v2",
		"asynq":       "github.com/hibiken/asynq",
	}

	for sym, path := range stdPackages {
		ai.KnownPackages[sym] = path
	}
	for sym, path := range thirdParty {
		ai.KnownPackages[sym] = path
	}

	return ai
}

// Resolve finds undefined symbols in the given code and returns the import
// fixes needed to resolve them.
func (ai *AutoImporter) Resolve(code string) []ImportFix {
	missing := ai.DetectMissing(code)
	if len(missing) == 0 {
		return nil
	}

	ai.mu.RLock()
	defer ai.mu.RUnlock()

	var fixes []ImportFix
	seen := make(map[string]bool)

	for _, symbol := range missing {
		parts := strings.SplitN(symbol, ".", 2)
		if len(parts) < 2 {
			continue
		}
		pkgName := parts[0]

		if seen[pkgName] {
			continue
		}

		if importPath, ok := ai.KnownPackages[pkgName]; ok {
			seen[pkgName] = true
			line := findSymbolLine(code, symbol)
			fixes = append(fixes, ImportFix{
				Package: pkgName,
				Path:    importPath,
				Symbol:  symbol,
				Line:    line,
			})
		}
	}

	// Sort fixes for deterministic output.
	sort.Slice(fixes, func(i, j int) bool {
		return fixes[i].Path < fixes[j].Path
	})

	return fixes
}

// ApplyFixes adds missing imports to the code, creating or extending the
// import block as needed. Imports are grouped: stdlib, external, internal.
func (ai *AutoImporter) ApplyFixes(code string, fixes []ImportFix) string {
	if len(fixes) == 0 {
		return code
	}

	// Collect already-imported packages.
	existingImports := extractExistingImports(code)

	// Determine which fixes are actually needed.
	var needed []ImportFix
	for _, fix := range fixes {
		if !existingImports[fix.Path] {
			needed = append(needed, fix)
		}
	}
	if len(needed) == 0 {
		return code
	}

	// Group new imports.
	var stdlib, external, internal []string
	for _, fix := range needed {
		if isGoStdlib(fix.Path) {
			stdlib = append(stdlib, fix.Path)
		} else if isInternalImport(code, fix.Path) {
			internal = append(internal, fix.Path)
		} else {
			external = append(external, fix.Path)
		}
	}
	sort.Strings(stdlib)
	sort.Strings(external)
	sort.Strings(internal)

	// Check if there's an existing import block.
	groupedImportRe := regexp.MustCompile(`(?ms)^import\s*\(\s*\n(.*?)\)\s*\n`)
	singleImportRe := regexp.MustCompile(`(?m)^import\s+"[^"]+"\s*\n`)

	if loc := groupedImportRe.FindStringIndex(code); loc != nil {
		// Parse existing grouped imports and merge.
		return mergeIntoGroupedImport(code, loc, stdlib, external, internal)
	}

	if locs := singleImportRe.FindAllStringIndex(code, -1); len(locs) > 0 {
		// Convert single imports to grouped and add new ones.
		return convertAndMergeImports(code, locs, stdlib, external, internal)
	}

	// No import block exists; create one after the package declaration.
	return createImportBlock(code, stdlib, external, internal)
}

// DetectMissing finds package-qualified calls in code that don't have a
// corresponding import statement (e.g., fmt.Println without "fmt" imported).
func (ai *AutoImporter) DetectMissing(code string) []string {
	// Find all package-qualified references (e.g., fmt.Println, http.Get).
	qualifiedRe := regexp.MustCompile(`\b([a-zA-Z][a-zA-Z0-9_]*)\.\s*([A-Z][a-zA-Z0-9_]*)`)
	matches := qualifiedRe.FindAllStringSubmatch(code, -1)
	if len(matches) == 0 {
		return nil
	}

	// Extract existing imports.
	existingImports := extractImportedPackageNames(code)

	// Collect missing symbols.
	seen := make(map[string]bool)
	var missing []string

	for _, m := range matches {
		pkgName := m[1]
		symbol := m[0]

		// Skip if the package is already imported.
		if existingImports[pkgName] {
			continue
		}

		// Skip common false positives (struct field access, etc.).
		if isLikelyFieldAccess(code, pkgName) {
			continue
		}

		if !seen[symbol] {
			seen[symbol] = true
			missing = append(missing, symbol)
		}
	}

	sort.Strings(missing)
	return missing
}

// SuggestImport returns possible import paths for a given symbol (package name).
func (ai *AutoImporter) SuggestImport(symbol string) []string {
	ai.mu.RLock()
	defer ai.mu.RUnlock()

	// Direct match.
	if path, ok := ai.KnownPackages[symbol]; ok {
		return []string{path}
	}

	// Partial/fuzzy match: find packages containing the symbol as substring.
	var suggestions []string
	symbolLower := strings.ToLower(symbol)
	for pkg, path := range ai.KnownPackages {
		if strings.Contains(strings.ToLower(pkg), symbolLower) {
			suggestions = append(suggestions, path)
		}
	}

	sort.Strings(suggestions)
	return suggestions
}

// RegisterPackage adds a new symbol-to-import-path mapping.
func (ai *AutoImporter) RegisterPackage(symbol, importPath string) {
	ai.mu.Lock()
	defer ai.mu.Unlock()
	ai.KnownPackages[symbol] = importPath
}

// FormatFixes renders a human-readable summary of the import fixes.
func (ai *AutoImporter) FormatFixes(fixes []ImportFix) string {
	if len(fixes) == 0 {
		return "No missing imports detected."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d missing import(s):\n", len(fixes)))
	for i, fix := range fixes {
		b.WriteString(fmt.Sprintf("  %d. %q (package %s) — used by %s",
			i+1, fix.Path, fix.Package, fix.Symbol))
		if fix.Line > 0 {
			b.WriteString(fmt.Sprintf(" at line %d", fix.Line))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// --- Helper functions ---

// extractExistingImports returns a set of import paths already present in the code.
func extractExistingImports(code string) map[string]bool {
	imports := make(map[string]bool)

	// Match grouped imports.
	groupedRe := regexp.MustCompile(`(?ms)^import\s*\(\s*\n(.*?)\)`)
	if m := groupedRe.FindStringSubmatch(code); len(m) > 1 {
		pathRe := regexp.MustCompile(`"([^"]+)"`)
		for _, pm := range pathRe.FindAllStringSubmatch(m[1], -1) {
			imports[pm[1]] = true
		}
	}

	// Match single imports.
	singleRe := regexp.MustCompile(`(?m)^import\s+(?:\w+\s+)?"([^"]+)"`)
	for _, m := range singleRe.FindAllStringSubmatch(code, -1) {
		imports[m[1]] = true
	}

	return imports
}

// extractImportedPackageNames returns a set of package names that are imported.
func extractImportedPackageNames(code string) map[string]bool {
	names := make(map[string]bool)

	// Match grouped imports.
	groupedRe := regexp.MustCompile(`(?ms)^import\s*\(\s*\n(.*?)\)`)
	if m := groupedRe.FindStringSubmatch(code); len(m) > 1 {
		lineRe := regexp.MustCompile(`(?m)^\s*(?:(\w+)\s+)?"([^"]+)"`)
		for _, lm := range lineRe.FindAllStringSubmatch(m[1], -1) {
			alias := lm[1]
			path := lm[2]
			if alias != "" && alias != "_" {
				names[alias] = true
			} else {
				// Use last segment of path as package name.
				parts := strings.Split(path, "/")
				pkgName := parts[len(parts)-1]
				names[pkgName] = true
			}
		}
	}

	// Match single imports.
	singleRe := regexp.MustCompile(`(?m)^import\s+(?:(\w+)\s+)?"([^"]+)"`)
	for _, m := range singleRe.FindAllStringSubmatch(code, -1) {
		alias := m[1]
		path := m[2]
		if alias != "" && alias != "_" {
			names[alias] = true
		} else {
			parts := strings.Split(path, "/")
			pkgName := parts[len(parts)-1]
			names[pkgName] = true
		}
	}

	return names
}

// isLikelyFieldAccess checks if a name is likely a variable/struct field rather
// than a package reference. It looks for lowercase variable declarations.
func isLikelyFieldAccess(code string, name string) bool {
	// If the name starts with a lowercase letter and is declared as a variable, skip it.
	if len(name) == 0 {
		return false
	}
	// Check if there's a var/short-decl for this name.
	declRe := regexp.MustCompile(`(?m)(?:var\s+` + regexp.QuoteMeta(name) + `\b|` + regexp.QuoteMeta(name) + `\s*:=|` + regexp.QuoteMeta(name) + `\s+\w+.*\{)`)
	return declRe.MatchString(code)
}

// findSymbolLine returns the line number where a symbol first appears.
func findSymbolLine(code, symbol string) int {
	lines := strings.Split(code, "\n")
	for i, line := range lines {
		if strings.Contains(line, symbol) {
			return i + 1
		}
	}
	return 0
}

// isInternalImport checks if an import path appears to be internal to the
// project by looking at the module path in the code.
func isInternalImport(code string, importPath string) bool {
	// Try to detect module path from existing imports.
	groupedRe := regexp.MustCompile(`(?ms)^import\s*\(\s*\n(.*?)\)`)
	if m := groupedRe.FindStringSubmatch(code); len(m) > 1 {
		pathRe := regexp.MustCompile(`"([^"]+)"`)
		for _, pm := range pathRe.FindAllStringSubmatch(m[1], -1) {
			existingPath := pm[1]
			if !isGoStdlib(existingPath) && len(existingPath) > 0 {
				// Extract module root (first 3 path segments).
				parts := strings.Split(existingPath, "/")
				if len(parts) >= 3 {
					moduleRoot := strings.Join(parts[:3], "/")
					if strings.HasPrefix(importPath, moduleRoot) {
						return true
					}
				}
			}
		}
	}
	return false
}

// mergeIntoGroupedImport adds new imports into an existing grouped import block.
func mergeIntoGroupedImport(code string, loc []int, stdlib, external, internal []string) string {
	groupedRe := regexp.MustCompile(`(?ms)^import\s*\(\s*\n(.*?)\)\s*\n`)
	m := groupedRe.FindStringSubmatch(code)
	if len(m) < 2 {
		return code
	}

	existingBlock := m[1]

	// Parse existing imports into groups.
	existingPaths := make(map[string]bool)
	pathRe := regexp.MustCompile(`"([^"]+)"`)
	for _, pm := range pathRe.FindAllStringSubmatch(existingBlock, -1) {
		existingPaths[pm[1]] = true
	}

	// Build new import entries (only those not already present).
	var newStdlib, newExternal, newInternal []string
	for _, p := range stdlib {
		if !existingPaths[p] {
			newStdlib = append(newStdlib, p)
		}
	}
	for _, p := range external {
		if !existingPaths[p] {
			newExternal = append(newExternal, p)
		}
	}
	for _, p := range internal {
		if !existingPaths[p] {
			newInternal = append(newInternal, p)
		}
	}

	// Append new imports before the closing paren.
	var additions strings.Builder
	if len(newStdlib) > 0 {
		for _, p := range newStdlib {
			additions.WriteString(fmt.Sprintf("\t%q\n", p))
		}
	}
	if len(newExternal) > 0 {
		if additions.Len() > 0 || hasNonStdlibImport(existingBlock) {
			// Only add blank line if mixing groups.
		}
		for _, p := range newExternal {
			additions.WriteString(fmt.Sprintf("\n\t%q\n", p))
		}
	}
	if len(newInternal) > 0 {
		for _, p := range newInternal {
			additions.WriteString(fmt.Sprintf("\n\t%q\n", p))
		}
	}

	if additions.Len() == 0 {
		return code
	}

	// Insert before the closing paren.
	insertPoint := loc[1] - 2 // Before ")\n"
	for insertPoint > 0 && code[insertPoint] != '\n' {
		insertPoint--
	}
	if insertPoint > 0 {
		insertPoint++ // After the newline
	}

	result := code[:insertPoint] + additions.String() + code[insertPoint:]
	return result
}

// hasNonStdlibImport checks if a block contains non-stdlib imports.
func hasNonStdlibImport(block string) bool {
	pathRe := regexp.MustCompile(`"([^"]+)"`)
	for _, m := range pathRe.FindAllStringSubmatch(block, -1) {
		if !isGoStdlib(m[1]) {
			return true
		}
	}
	return false
}

// convertAndMergeImports converts single import statements to a grouped block
// with new imports added.
func convertAndMergeImports(code string, locs [][]int, stdlib, external, internal []string) string {
	// Extract existing single imports.
	singleRe := regexp.MustCompile(`(?m)^import\s+(?:(\w+)\s+)?"([^"]+)"`)
	matches := singleRe.FindAllStringSubmatch(code, -1)

	var existingStdlib, existingExternal, existingInternal []string
	existingPaths := make(map[string]bool)

	for _, m := range matches {
		path := m[2]
		existingPaths[path] = true
		if isGoStdlib(path) {
			existingStdlib = append(existingStdlib, path)
		} else if isInternalImport(code, path) {
			existingInternal = append(existingInternal, path)
		} else {
			existingExternal = append(existingExternal, path)
		}
	}

	// Add new imports (deduped).
	for _, p := range stdlib {
		if !existingPaths[p] {
			existingStdlib = append(existingStdlib, p)
		}
	}
	for _, p := range external {
		if !existingPaths[p] {
			existingExternal = append(existingExternal, p)
		}
	}
	for _, p := range internal {
		if !existingPaths[p] {
			existingInternal = append(existingInternal, p)
		}
	}

	sort.Strings(existingStdlib)
	sort.Strings(existingExternal)
	sort.Strings(existingInternal)

	// Build new import block.
	newBlock := buildImportBlock(existingStdlib, existingExternal, existingInternal)

	// Replace from first import to last import.
	start := locs[0][0]
	end := locs[len(locs)-1][1]
	return code[:start] + newBlock + code[end:]
}

// createImportBlock creates a new import block after the package declaration.
func createImportBlock(code string, stdlib, external, internal []string) string {
	pkgRe := regexp.MustCompile(`(?m)^package\s+\w+\s*\n`)
	loc := pkgRe.FindStringIndex(code)
	if loc == nil {
		// Fallback: prepend.
		return buildImportBlock(stdlib, external, internal) + "\n" + code
	}

	newBlock := "\n" + buildImportBlock(stdlib, external, internal)
	return code[:loc[1]] + newBlock + code[loc[1]:]
}

// buildImportBlock constructs a properly grouped import block.
func buildImportBlock(stdlib, external, internal []string) string {
	total := len(stdlib) + len(external) + len(internal)
	if total == 0 {
		return ""
	}

	if total == 1 {
		var path string
		if len(stdlib) > 0 {
			path = stdlib[0]
		} else if len(external) > 0 {
			path = external[0]
		} else {
			path = internal[0]
		}
		return fmt.Sprintf("import %q\n", path)
	}

	var b strings.Builder
	b.WriteString("import (\n")

	wroteGroup := false
	if len(stdlib) > 0 {
		for _, p := range stdlib {
			b.WriteString(fmt.Sprintf("\t%q\n", p))
		}
		wroteGroup = true
	}
	if len(external) > 0 {
		if wroteGroup {
			b.WriteString("\n")
		}
		for _, p := range external {
			b.WriteString(fmt.Sprintf("\t%q\n", p))
		}
		wroteGroup = true
	}
	if len(internal) > 0 {
		if wroteGroup {
			b.WriteString("\n")
		}
		for _, p := range internal {
			b.WriteString(fmt.Sprintf("\t%q\n", p))
		}
	}

	b.WriteString(")\n")
	return b.String()
}

// --- AutoImportTool implements Tool interface ---

// AutoImportTool resolves and adds missing imports in Go source files.
type AutoImportTool struct {
	importer *AutoImporter
}

// AutoImportInput is the typed input for AutoImportTool.
type AutoImportInput struct {
	Code  string `json:"code"`
	File  string `json:"file"`
	Apply bool   `json:"apply"`
}

// NewAutoImportTool creates an AutoImportTool with a pre-configured AutoImporter.
func NewAutoImportTool() *AutoImportTool {
	return &AutoImportTool{
		importer: NewAutoImporter(),
	}
}

func (AutoImportTool) Name() string { return "AutoImport" }
func (AutoImportTool) Description() string {
	return "Automatically detect and add missing imports in Go source code. Analyzes package-qualified symbol references and adds the corresponding import statements."
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (AutoImportTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"code":  {Type: "string", Description: "Go source code to analyze for missing imports"},
			"file":  {Type: "string", Description: "Optional file path for context (used in fix reporting)"},
			"apply": {Type: "boolean", Description: "If true, return the code with fixes applied; if false, only report missing imports"},
		},
		Required: []string{"code"},
	}
}

func (AutoImportTool) Parameters() map[string]interface{} {
	return autoImportSchema.ToJSONSchema()
}

// autoImportSchema is the single source of truth for AutoImport's input schema.
var autoImportSchema = AutoImportTool{}.Schema()

func (t AutoImportTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[AutoImportInput]("AutoImport", input)
	if err != nil {
		return "", err
	}
	if p.Code == "" {
		return "", fmt.Errorf("code is required")
	}

	fixes := t.importer.Resolve(p.Code)

	// Set file path on fixes if provided.
	if p.File != "" {
		for i := range fixes {
			fixes[i].File = p.File
		}
	}

	if p.Apply {
		result := t.importer.ApplyFixes(p.Code, fixes)
		return result, nil
	}

	return t.importer.FormatFixes(fixes), nil
}

// Ensure AutoImportTool satisfies Tool interface at compile time.
var _ Tool = AutoImportTool{}
