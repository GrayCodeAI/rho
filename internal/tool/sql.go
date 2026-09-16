package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	// SQLite driver (pure Go) is already a direct dependency of rho and is
	// registered under the "sqlite" driver name.
	_ "modernc.org/sqlite"
)

// SQLTool is a built-in tool for exploring relational databases. It connects to
// a database via database/sql, runs a single query, and returns the rows
// formatted as a plain-text table.
//
// Only the SQLite driver ships with rho today (via modernc.org/sqlite). The
// Postgres and MySQL dialects are recognized so callers get a clear error
// instead of a confusing "unknown driver" failure: pull in a driver and the
// dialect light up without further changes here.
type SQLTool struct{}

// SQLInput is the typed input for SQLTool.
type SQLInput struct {
	Driver     string `json:"driver"`
	DSN        string `json:"dsn"`
	Query      string `json:"query"`
	AllowWrite bool   `json:"allow_write"`
	MaxRows    int    `json:"max_rows"`
}

func (SQLTool) Name() string      { return "SQL" }
func (SQLTool) Aliases() []string { return []string{"sql", "sql_query"} }

func (SQLTool) Description() string {
	return "Run a SQL query against a SQLite, Postgres, or MySQL database and " +
		"return the rows as a table. Read-only by default: destructive " +
		"statements (INSERT/UPDATE/DELETE/DROP/...) are blocked unless " +
		"allow_write is set to true. Only the SQLite driver is bundled; other " +
		"dialects require their driver to be compiled in."
}

// SQLTool is read-only by default and so is low risk, but writes are possible
// when explicitly allowed; treat it as medium overall so confirmation flows
// can prompt when appropriate.
func (SQLTool) RiskLevel() string { return "medium" }

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (SQLTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"driver":      {Type: "string", Enum: []interface{}{"sqlite", "postgres", "mysql"}, Description: "Database dialect: one of sqlite, postgres, mysql. Defaults to sqlite."},
			"dsn":         {Type: "string", Description: `Data source name / connection string. For sqlite this is the file path (or ":memory:").`},
			"query":       {Type: "string", Description: "The SQL statement to execute."},
			"allow_write": {Type: "boolean", Description: "Allow destructive / mutating statements. When false (the default) only read-only queries are permitted."},
			"max_rows":    {Type: "integer", Description: "Maximum number of rows to return (default 100)."},
		},
		Required: []string{"dsn", "query"},
	}
}

func (SQLTool) Parameters() map[string]interface{} {
	return sqlSchema.ToJSONSchema()
}

// sqlSchema is the single source of truth for SQL's input schema.
var sqlSchema = SQLTool{}.Schema()

// driverName maps a user-facing dialect to its database/sql driver name. The
// bool reports whether rho bundles a driver for that dialect.
func sqlDriverName(dialect string) (driver string, bundled bool, err error) {
	switch strings.ToLower(strings.TrimSpace(dialect)) {
	case "", "sqlite", "sqlite3":
		return "sqlite", true, nil
	case "postgres", "postgresql", "pgx":
		return "postgres", false, nil
	case "mysql", "mariadb":
		return "mysql", false, nil
	default:
		return "", false, fmt.Errorf("unsupported driver %q (want sqlite, postgres, or mysql)", dialect)
	}
}

// writeVerbs are statement leading keywords that mutate state. A query whose
// first token matches one of these is rejected unless allow_write is set.
var writeVerbs = map[string]bool{
	"insert":   true,
	"update":   true,
	"delete":   true,
	"drop":     true,
	"alter":    true,
	"create":   true,
	"truncate": true,
	"replace":  true,
	"merge":    true,
	"grant":    true,
	"revoke":   true,
	"attach":   true,
	"detach":   true,
	"vacuum":   true,
	"reindex":  true,
	"pragma":   true,
}

// isReadOnlyQuery reports whether the statement is safe to run in read-only
// mode. It inspects the first meaningful keyword after stripping leading
// comments and whitespace.
func isReadOnlyQuery(query string) bool {
	statements, err := splitSQLStatements(query)
	if err != nil || len(statements) != 1 {
		return false
	}
	first := firstSQLKeyword(statements[0])
	if first == "" {
		return false
	}
	// Allow read-only CTEs. SQLite's connection-level query_only guard still
	// blocks mutations when allow_write is false, and splitSQLStatements keeps
	// stacked statements out.
	if first == "with" {
		return true
	}
	return !writeVerbs[first]
}

// splitSQLStatements accepts one SQL statement and ignores semicolons inside
// quoted strings and comments. SQLite permits stacked statements, so callers
// must reject more than one statement even when writes are explicitly allowed.
func splitSQLStatements(query string) ([]string, error) {
	var statements []string
	start := 0
	var quote byte
	lineComment, blockComment := false, false
	for i := 0; i < len(query); i++ {
		c := query[i]
		if lineComment {
			if c == '\n' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if c == '*' && i+1 < len(query) && query[i+1] == '/' {
				blockComment = false
				i++
			}
			continue
		}
		if quote != 0 {
			if c == quote {
				if i+1 < len(query) && query[i+1] == quote {
					i++ // SQL escapes a quote by doubling it.
				} else {
					quote = 0
				}
			}
			continue
		}
		switch c {
		case '-', '/':
			if c == '-' && i+1 < len(query) && query[i+1] == '-' {
				lineComment = true
				i++
			} else if c == '/' && i+1 < len(query) && query[i+1] == '*' {
				blockComment = true
				i++
			}
		case '\'', '"', '`':
			quote = c
		case ';':
			if statement := strings.TrimSpace(query[start:i]); statement != "" {
				statements = append(statements, statement)
			}
			start = i + 1
		}
	}
	if blockComment || quote != 0 {
		return nil, fmt.Errorf("query contains an unterminated SQL comment or quoted string")
	}
	if statement := strings.TrimSpace(query[start:]); statement != "" {
		statements = append(statements, statement)
	}
	return statements, nil
}

func validateSQLiteDSN(ctx context.Context, dsn string) error {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == ":memory:" || strings.HasPrefix(trimmed, "file::memory:") {
		return nil
	}
	path := trimmed
	if strings.HasPrefix(trimmed, "file:") {
		u, err := url.Parse(trimmed)
		if err != nil || u.Host != "" {
			return fmt.Errorf("sqlite dsn must reference a local file")
		}
		path = u.Path
		if path == "" {
			return fmt.Errorf("sqlite file dsn is missing a path")
		}
		if decoded, err := url.PathUnescape(path); err == nil {
			path = decoded
		}
	}
	if err := validatePathAllowed(ctx, path); err != nil {
		return fmt.Errorf("sqlite dsn: %w", err)
	}
	return nil
}

// firstSQLKeyword returns the lowercased first keyword of a statement, skipping
// leading line (--) and block (/* */) comments and whitespace.
func firstSQLKeyword(query string) string {
	s := query
	for {
		s = strings.TrimLeft(s, " \t\r\n;")
		switch {
		case strings.HasPrefix(s, "--"):
			if idx := strings.IndexByte(s, '\n'); idx >= 0 {
				s = s[idx+1:]
				continue
			}
			return ""
		case strings.HasPrefix(s, "/*"):
			if idx := strings.Index(s, "*/"); idx >= 0 {
				s = s[idx+2:]
				continue
			}
			return ""
		}
		break
	}
	// Read the leading word.
	end := 0
	for end < len(s) {
		c := s[end]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			end++
			continue
		}
		break
	}
	return strings.ToLower(s[:end])
}

func (t SQLTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[SQLInput]("SQL", input)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(p.DSN) == "" {
		return "", fmt.Errorf("dsn is required")
	}
	if strings.TrimSpace(p.Query) == "" {
		return "", fmt.Errorf("query is required")
	}
	if p.MaxRows <= 0 {
		p.MaxRows = 100
	}

	driver, bundled, err := sqlDriverName(p.Driver)
	if err != nil {
		return "", err
	}
	if !bundled {
		return "", fmt.Errorf("the %s driver is not compiled into this build of rho; only sqlite is available", driver)
	}

	statements, err := splitSQLStatements(p.Query)
	if err != nil {
		return "", fmt.Errorf("invalid query: %w", err)
	}
	if len(statements) != 1 {
		return "", fmt.Errorf("exactly one SQL statement is required")
	}
	if !p.AllowWrite && !isReadOnlyQuery(statements[0]) {
		return "", fmt.Errorf("refusing to run a destructive statement in read-only mode; set allow_write=true to override")
	}
	if driver == "sqlite" {
		if err := validateSQLiteDSN(ctx, p.DSN); err != nil {
			return "", err
		}
	}

	db, err := sql.Open(driver, p.DSN)
	if err != nil {
		return "", fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !p.AllowWrite {
		// PRAGMA query_only is connection-local. Restrict the pool to one
		// connection so the guard applies to the subsequent query as well.
		db.SetMaxOpenConns(1)
		if _, err := db.ExecContext(queryCtx, "PRAGMA query_only=ON"); err != nil {
			return "", fmt.Errorf("enable sqlite read-only mode: %w", err)
		}
	}

	return runSQLQuery(queryCtx, db, statements[0], p.MaxRows)
}

// querier is the subset of *sql.DB used by runSQLQuery, extracted so tests can
// supply an alternative if desired.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// runSQLQuery executes the query and renders the result set as a table.
func runSQLQuery(ctx context.Context, db querier, query string, maxRows int) (string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("columns: %w", err)
	}
	if len(cols) == 0 {
		return "(statement executed; no result columns)", nil
	}

	var table [][]string
	table = append(table, cols)

	truncated := false
	count := 0
	for rows.Next() {
		if count >= maxRows {
			truncated = true
			break
		}
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", fmt.Errorf("scan: %w", err)
		}
		row := make([]string, len(cols))
		for i, v := range raw {
			row[i] = formatSQLValue(v)
		}
		table = append(table, row)
		count++
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate rows: %w", err)
	}

	out := renderSQLTable(table)
	if truncated {
		out += fmt.Sprintf("\n... (truncated at %d rows)", maxRows)
	} else {
		out += fmt.Sprintf("\n(%d row(s))", count)
	}
	return out, nil
}

// formatSQLValue renders a scanned column value as a display string.
func formatSQLValue(v any) string {
	switch val := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(val)
	case time.Time:
		return val.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// renderSQLTable formats rows (first row treated as the header) into an
// aligned, pipe-delimited text table.
func renderSQLTable(table [][]string) string {
	if len(table) == 0 {
		return ""
	}
	widths := make([]int, len(table[0]))
	for _, row := range table {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	var b strings.Builder
	writeRow := func(row []string) {
		for i, cell := range row {
			if i > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(cell)
			if i < len(widths) {
				b.WriteString(strings.Repeat(" ", widths[i]-len(cell)))
			}
		}
		b.WriteByte('\n')
	}

	writeRow(table[0])
	// Separator under the header.
	for i := range table[0] {
		if i > 0 {
			b.WriteString("-+-")
		}
		b.WriteString(strings.Repeat("-", widths[i]))
	}
	b.WriteByte('\n')
	for _, row := range table[1:] {
		writeRow(row)
	}
	return strings.TrimRight(b.String(), "\n")
}
