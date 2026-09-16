// Package tool provides built-in tools for the rho coding agent.
//
// Deprecated APIs: parser.ParseDir and ast.Package are deprecated since Go 1.25/1.22.
// A future refactor should migrate to golang.org/x/tools/go/packages, but the current
// implementation is functional and the migration is non-trivial.
//
//nolint:staticcheck
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GrayCodeAI/rho/internal/ui/icons"
)

// APISnapshot captures the full public API surface of a Go package.
type APISnapshot struct {
	Package     string         `json:"package"`
	Functions   []FuncSig      `json:"functions"`
	Types       []TypeSig      `json:"types"`
	Interfaces  []InterfaceSig `json:"interfaces"`
	Constants   []ConstSig     `json:"constants"`
	Variables   []VarSig       `json:"variables"`
	GeneratedAt time.Time      `json:"generated_at"`
}

// FuncSig represents a function or method signature.
type FuncSig struct {
	Name     string   `json:"name"`
	Receiver string   `json:"receiver"`
	Params   []string `json:"params"`
	Returns  []string `json:"returns"`
	Exported bool     `json:"exported"`
}

// TypeSig represents a type declaration.
type TypeSig struct {
	Name     string     `json:"name"`
	Kind     string     `json:"kind"`
	Fields   []FieldSig `json:"fields"`
	Exported bool       `json:"exported"`
}

// InterfaceSig represents an interface declaration.
type InterfaceSig struct {
	Name     string    `json:"name"`
	Methods  []FuncSig `json:"methods"`
	Exported bool      `json:"exported"`
}

// FieldSig represents a struct field.
type FieldSig struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Tag      string `json:"tag"`
	Exported bool   `json:"exported"`
}

// ConstSig represents a constant declaration.
type ConstSig struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Exported bool   `json:"exported"`
}

// VarSig represents a variable declaration.
type VarSig struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Exported bool   `json:"exported"`
}

// BreakingChange describes a single incompatible change between two API snapshots.
type BreakingChange struct {
	Type     string `json:"type"` // "removed", "signature_changed", "type_changed", "field_removed", "method_added"
	Symbol   string `json:"symbol"`
	Old      string `json:"old"`
	New      string `json:"new"`
	Severity string `json:"severity"` // "breaking", "deprecated", "compatible"
}

// CompatChecker performs API compatibility analysis between snapshots.
type CompatChecker struct {
	mu sync.Mutex
}

// NewCompatChecker creates a new CompatChecker instance.
func NewCompatChecker() *CompatChecker {
	return &CompatChecker{}
}

// Snapshot parses the Go package at the given directory path and extracts the public API.
func (c *CompatChecker) Snapshot(path string) (*APISnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	fset := token.NewFileSet()
	//lint:ignore SA1019 parser.ParseDir is deprecated; migration to go/packages is non-trivial
	pkgs, err := parser.ParseDir(fset, path, func(fi os.FileInfo) bool {
		name := fi.Name()
		return !strings.HasSuffix(name, "_test.go")
	}, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing package at %s: %w", path, err)
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no Go packages found in %s", path)
	}

	// Take the first package found.
	//lint:ignore SA1019 ast.Package is deprecated; migration to go/types is non-trivial
	var pkg *ast.Package
	var pkgName string
	for name, p := range pkgs {
		pkgName = name
		pkg = p
		break
	}

	snap := &APISnapshot{
		Package:     pkgName,
		GeneratedAt: time.Now(),
	}

	for _, file := range pkg.Files {
		c.extractFile(file, snap)
	}

	sort.Slice(snap.Functions, func(i, j int) bool {
		return snap.Functions[i].Name < snap.Functions[j].Name
	})
	sort.Slice(snap.Types, func(i, j int) bool {
		return snap.Types[i].Name < snap.Types[j].Name
	})
	sort.Slice(snap.Interfaces, func(i, j int) bool {
		return snap.Interfaces[i].Name < snap.Interfaces[j].Name
	})
	sort.Slice(snap.Constants, func(i, j int) bool {
		return snap.Constants[i].Name < snap.Constants[j].Name
	})
	sort.Slice(snap.Variables, func(i, j int) bool {
		return snap.Variables[i].Name < snap.Variables[j].Name
	})

	return snap, nil
}

func (c *CompatChecker) extractFile(file *ast.File, snap *APISnapshot) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			sig := c.extractFuncSig(d)
			if sig.Exported {
				snap.Functions = append(snap.Functions, sig)
			}
		case *ast.GenDecl:
			switch d.Tok {
			case token.TYPE:
				for _, spec := range d.Specs {
					ts, _ := spec.(*ast.TypeSpec)
					if !ast.IsExported(ts.Name.Name) {
						continue
					}
					switch t := ts.Type.(type) {
					case *ast.InterfaceType:
						iface := c.extractInterface(ts.Name.Name, t)
						snap.Interfaces = append(snap.Interfaces, iface)
					case *ast.StructType:
						typeSig := c.extractStruct(ts.Name.Name, t)
						snap.Types = append(snap.Types, typeSig)
					default:
						snap.Types = append(snap.Types, TypeSig{
							Name:     ts.Name.Name,
							Kind:     exprToString(ts.Type),
							Exported: true,
						})
					}
				}
			case token.CONST:
				for _, spec := range d.Specs {
					vs, _ := spec.(*ast.ValueSpec)
					typeStr := ""
					if vs.Type != nil {
						typeStr = exprToString(vs.Type)
					}
					for _, name := range vs.Names {
						if ast.IsExported(name.Name) {
							snap.Constants = append(snap.Constants, ConstSig{
								Name:     name.Name,
								Type:     typeStr,
								Exported: true,
							})
						}
					}
				}
			case token.VAR:
				for _, spec := range d.Specs {
					vs, _ := spec.(*ast.ValueSpec)
					typeStr := ""
					if vs.Type != nil {
						typeStr = exprToString(vs.Type)
					}
					for _, name := range vs.Names {
						if ast.IsExported(name.Name) {
							snap.Variables = append(snap.Variables, VarSig{
								Name:     name.Name,
								Type:     typeStr,
								Exported: true,
							})
						}
					}
				}
			}
		}
	}
}

func (c *CompatChecker) extractFuncSig(d *ast.FuncDecl) FuncSig {
	sig := FuncSig{
		Name:     d.Name.Name,
		Exported: ast.IsExported(d.Name.Name),
	}

	if d.Recv != nil && len(d.Recv.List) > 0 {
		sig.Receiver = exprToString(d.Recv.List[0].Type)
	}

	if d.Type.Params != nil {
		for _, field := range d.Type.Params.List {
			typeStr := exprToString(field.Type)
			if len(field.Names) == 0 {
				sig.Params = append(sig.Params, typeStr)
			} else {
				for range field.Names {
					sig.Params = append(sig.Params, typeStr)
				}
			}
		}
	}

	if d.Type.Results != nil {
		for _, field := range d.Type.Results.List {
			typeStr := exprToString(field.Type)
			if len(field.Names) == 0 {
				sig.Returns = append(sig.Returns, typeStr)
			} else {
				for range field.Names {
					sig.Returns = append(sig.Returns, typeStr)
				}
			}
		}
	}

	return sig
}

func (c *CompatChecker) extractInterface(name string, iface *ast.InterfaceType) InterfaceSig {
	sig := InterfaceSig{
		Name:     name,
		Exported: true,
	}

	if iface.Methods != nil {
		for _, method := range iface.Methods.List {
			if len(method.Names) == 0 {
				// Embedded interface
				continue
			}
			fn, ok := method.Type.(*ast.FuncType)
			if !ok {
				continue
			}
			msig := FuncSig{
				Name:     method.Names[0].Name,
				Exported: ast.IsExported(method.Names[0].Name),
			}
			if fn.Params != nil {
				for _, field := range fn.Params.List {
					typeStr := exprToString(field.Type)
					if len(field.Names) == 0 {
						msig.Params = append(msig.Params, typeStr)
					} else {
						for range field.Names {
							msig.Params = append(msig.Params, typeStr)
						}
					}
				}
			}
			if fn.Results != nil {
				for _, field := range fn.Results.List {
					typeStr := exprToString(field.Type)
					if len(field.Names) == 0 {
						msig.Returns = append(msig.Returns, typeStr)
					} else {
						for range field.Names {
							msig.Returns = append(msig.Returns, typeStr)
						}
					}
				}
			}
			sig.Methods = append(sig.Methods, msig)
		}
	}

	return sig
}

func (c *CompatChecker) extractStruct(name string, st *ast.StructType) TypeSig {
	sig := TypeSig{
		Name:     name,
		Kind:     "struct",
		Exported: true,
	}

	if st.Fields != nil {
		for _, field := range st.Fields.List {
			typeStr := exprToString(field.Type)
			tag := ""
			if field.Tag != nil {
				tag = field.Tag.Value
			}
			if len(field.Names) == 0 {
				// Embedded field
				sig.Fields = append(sig.Fields, FieldSig{
					Name:     typeStr,
					Type:     typeStr,
					Tag:      tag,
					Exported: ast.IsExported(typeStr),
				})
			} else {
				for _, fname := range field.Names {
					sig.Fields = append(sig.Fields, FieldSig{
						Name:     fname.Name,
						Type:     typeStr,
						Tag:      tag,
						Exported: ast.IsExported(fname.Name),
					})
				}
			}
		}
	}

	return sig
}

// Compare detects breaking changes between two API snapshots.
func (c *CompatChecker) Compare(old, new *APISnapshot) []BreakingChange {
	c.mu.Lock()
	defer c.mu.Unlock()

	var changes []BreakingChange

	// Check removed and changed functions.
	newFuncs := make(map[string]FuncSig)
	for _, f := range new.Functions {
		key := f.Name
		if f.Receiver != "" {
			key = f.Receiver + "." + f.Name
		}
		newFuncs[key] = f
	}
	for _, f := range old.Functions {
		key := f.Name
		if f.Receiver != "" {
			key = f.Receiver + "." + f.Name
		}
		nf, exists := newFuncs[key]
		if !exists {
			changes = append(changes, BreakingChange{
				Type:     "removed",
				Symbol:   "func " + key,
				Old:      formatFuncSig(f),
				New:      "",
				Severity: "breaking",
			})
		} else {
			if !funcSigsEqual(f, nf) {
				changes = append(changes, BreakingChange{
					Type:     "signature_changed",
					Symbol:   "func " + key,
					Old:      formatFuncSig(f),
					New:      formatFuncSig(nf),
					Severity: "breaking",
				})
			}
		}
	}

	// Check for new functions (compatible addition).
	oldFuncs := make(map[string]FuncSig)
	for _, f := range old.Functions {
		key := f.Name
		if f.Receiver != "" {
			key = f.Receiver + "." + f.Name
		}
		oldFuncs[key] = f
	}
	for _, f := range new.Functions {
		key := f.Name
		if f.Receiver != "" {
			key = f.Receiver + "." + f.Name
		}
		if _, exists := oldFuncs[key]; !exists {
			changes = append(changes, BreakingChange{
				Type:     "removed", // using "removed" with compatible severity for additions
				Symbol:   "func " + key,
				Old:      "",
				New:      formatFuncSig(f),
				Severity: "compatible",
			})
		}
	}

	// Check removed and changed types.
	newTypes := make(map[string]TypeSig)
	for _, t := range new.Types {
		newTypes[t.Name] = t
	}
	for _, t := range old.Types {
		nt, exists := newTypes[t.Name]
		if !exists {
			changes = append(changes, BreakingChange{
				Type:     "type_changed",
				Symbol:   "type " + t.Name,
				Old:      t.Kind,
				New:      "",
				Severity: "breaking",
			})
		} else {
			// Check for removed fields in structs.
			if t.Kind == "struct" && nt.Kind == "struct" {
				newFields := make(map[string]FieldSig)
				for _, f := range nt.Fields {
					newFields[f.Name] = f
				}
				for _, f := range t.Fields {
					if !f.Exported {
						continue
					}
					nf, exists := newFields[f.Name]
					if !exists {
						changes = append(changes, BreakingChange{
							Type:     "field_removed",
							Symbol:   t.Name + "." + f.Name,
							Old:      f.Type,
							New:      "",
							Severity: "breaking",
						})
					} else if f.Type != nf.Type {
						changes = append(changes, BreakingChange{
							Type:     "type_changed",
							Symbol:   t.Name + "." + f.Name,
							Old:      f.Type,
							New:      nf.Type,
							Severity: "breaking",
						})
					}
				}
			} else if t.Kind != nt.Kind {
				changes = append(changes, BreakingChange{
					Type:     "type_changed",
					Symbol:   "type " + t.Name,
					Old:      t.Kind,
					New:      nt.Kind,
					Severity: "breaking",
				})
			}
		}
	}

	// Check interfaces for removed and added methods.
	newIfaces := make(map[string]InterfaceSig)
	for _, i := range new.Interfaces {
		newIfaces[i.Name] = i
	}
	for _, i := range old.Interfaces {
		ni, exists := newIfaces[i.Name]
		if !exists {
			changes = append(changes, BreakingChange{
				Type:     "removed",
				Symbol:   "interface " + i.Name,
				Old:      formatInterfaceMethods(i),
				New:      "",
				Severity: "breaking",
			})
			continue
		}
		// Check for removed methods (breaking).
		newMethods := make(map[string]FuncSig)
		for _, m := range ni.Methods {
			newMethods[m.Name] = m
		}
		oldMethods := make(map[string]FuncSig)
		for _, m := range i.Methods {
			oldMethods[m.Name] = m
		}
		for _, m := range i.Methods {
			nm, exists := newMethods[m.Name]
			if !exists {
				changes = append(changes, BreakingChange{
					Type:     "removed",
					Symbol:   i.Name + "." + m.Name,
					Old:      formatFuncSig(m),
					New:      "",
					Severity: "breaking",
				})
			} else if !funcSigsEqual(m, nm) {
				changes = append(changes, BreakingChange{
					Type:     "signature_changed",
					Symbol:   i.Name + "." + m.Name,
					Old:      formatFuncSig(m),
					New:      formatFuncSig(nm),
					Severity: "breaking",
				})
			}
		}
		// Check for added methods (breaks implementors).
		for _, m := range ni.Methods {
			if _, exists := oldMethods[m.Name]; !exists {
				changes = append(changes, BreakingChange{
					Type:     "method_added",
					Symbol:   "interface " + i.Name,
					Old:      "",
					New:      formatFuncSig(m),
					Severity: "breaking",
				})
			}
		}
	}

	return changes
}

// IsBackwardCompatible returns true if no breaking changes exist between old and new.
func (c *CompatChecker) IsBackwardCompatible(old, new *APISnapshot) bool {
	changes := c.Compare(old, new)
	for _, ch := range changes {
		if ch.Severity == "breaking" {
			return false
		}
	}
	return true
}

// FormatChanges produces a human-readable report of breaking changes.
func (c *CompatChecker) FormatChanges(changes []BreakingChange) string {
	var sb strings.Builder
	sb.WriteString("API Compatibility Check:\n")
	sb.WriteString("──────────────────────────\n")

	breaking := 0
	caution := 0
	compatible := 0

	for _, ch := range changes {
		switch ch.Severity {
		case "breaking":
			if ch.Type == "method_added" {
				caution++
				sb.WriteString(fmt.Sprintf(icons.Alert()+" CAUTION: %s method added (breaks implementors)\n", ch.Symbol))
				sb.WriteString(fmt.Sprintf("   added: %s\n", ch.New))
			} else {
				breaking++
				switch ch.Type {
				case "removed":
					sb.WriteString(fmt.Sprintf(icons.CloseThick()+" BREAKING: %s removed\n", ch.Symbol))
				case "signature_changed":
					sb.WriteString(fmt.Sprintf(icons.CloseThick()+" BREAKING: %s signature changed\n", ch.Symbol))
					sb.WriteString(fmt.Sprintf("   was: %s\n", ch.Old))
					sb.WriteString(fmt.Sprintf("   now: %s\n", ch.New))
				case "type_changed":
					sb.WriteString(fmt.Sprintf(icons.CloseThick()+" BREAKING: %s type changed\n", ch.Symbol))
					sb.WriteString(fmt.Sprintf("   was: %s\n", ch.Old))
					sb.WriteString(fmt.Sprintf("   now: %s\n", ch.New))
				case "field_removed":
					sb.WriteString(fmt.Sprintf(icons.CloseThick()+" BREAKING: %s field removed\n", ch.Symbol))
					sb.WriteString(fmt.Sprintf("   was: %s\n", ch.Old))
				}
			}
		case "compatible":
			compatible++
			sb.WriteString(fmt.Sprintf(icons.CheckBold()+" COMPATIBLE: %s added\n", ch.Symbol))
		}
	}

	sb.WriteString("──────────────────────────\n")
	sb.WriteString(fmt.Sprintf("Result: %d breaking, %d caution, %d compatible\n", breaking, caution, compatible))

	return sb.String()
}

// SaveSnapshot serializes an APISnapshot to a JSON file.
func (c *CompatChecker) SaveSnapshot(snapshot *APISnapshot, path string) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling snapshot: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing snapshot to %s: %w", path, err)
	}
	return nil
}

// LoadSnapshot deserializes an APISnapshot from a JSON file.
func (c *CompatChecker) LoadSnapshot(path string) (*APISnapshot, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path provided by caller via tool/task parameters, inherent to this dev CLI's file operations
	if err != nil {
		return nil, fmt.Errorf("reading snapshot from %s: %w", path, err)
	}
	var snap APISnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("unmarshaling snapshot: %w", err)
	}
	return &snap, nil
}

// APICompatTool implements the Tool interface for API compatibility checking.
type APICompatTool struct {
	checker *CompatChecker
}

// APICompatInput is the typed input for APICompatTool.
type APICompatInput struct {
	PackagePath  string `json:"package_path"`
	BaselinePath string `json:"baseline_path"`
	SaveBaseline bool   `json:"save_baseline"`
}

// NewAPICompatTool creates a new APICompatTool.
func NewAPICompatTool() *APICompatTool {
	return &APICompatTool{
		checker: NewCompatChecker(),
	}
}

func (t *APICompatTool) Name() string {
	return "APICompat"
}

func (t *APICompatTool) Description() string {
	return "Checks API compatibility between the current package and a saved baseline snapshot, detecting breaking changes."
}

func (t *APICompatTool) Parameters() map[string]interface{} {
	return apiCompatSchema.ToJSONSchema()
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (t APICompatTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"package_path":  {Type: "string", Description: "Path to the Go package directory to analyze"},
			"baseline_path": {Type: "string", Description: "Path to the baseline snapshot JSON file"},
			"save_baseline": {Type: "boolean", Description: "If true, save current API as baseline instead of comparing"},
		},
		Required: []string{"package_path"},
	}
}

// apiCompatSchema is the single source of truth for APICompat's input schema.
var apiCompatSchema = APICompatTool{}.Schema()

func (t *APICompatTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	params, err := DecodeInput[APICompatInput]("APICompat", input)
	if err != nil {
		return "", err
	}

	if params.PackagePath == "" {
		return "", fmt.Errorf("package_path is required")
	}

	current, err := t.checker.Snapshot(params.PackagePath)
	if err != nil {
		return "", fmt.Errorf("creating snapshot: %w", err)
	}

	if params.SaveBaseline {
		savePath := params.BaselinePath
		if savePath == "" {
			savePath = params.PackagePath + "/.api_baseline.json"
		}
		if saveErr := t.checker.SaveSnapshot(current, savePath); saveErr != nil {
			return "", saveErr
		}
		return fmt.Sprintf("Saved API baseline to %s (%d functions, %d types, %d interfaces)",
			savePath, len(current.Functions), len(current.Types), len(current.Interfaces)), nil
	}

	baselinePath := params.BaselinePath
	if baselinePath == "" {
		baselinePath = params.PackagePath + "/.api_baseline.json"
	}

	baseline, err := t.checker.LoadSnapshot(baselinePath)
	if err != nil {
		return "", fmt.Errorf("loading baseline: %w", err)
	}

	changes := t.checker.Compare(baseline, current)
	if len(changes) == 0 {
		return "API Compatibility Check: No changes detected. API is fully compatible.", nil
	}

	return t.checker.FormatChanges(changes), nil
}

// Helper functions

func exprToString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return exprToString(e.X) + "." + e.Sel.Name
	case *ast.StarExpr:
		return "*" + exprToString(e.X)
	case *ast.ArrayType:
		if e.Len == nil {
			return "[]" + exprToString(e.Elt)
		}
		return "[...]" + exprToString(e.Elt)
	case *ast.MapType:
		return "map[" + exprToString(e.Key) + "]" + exprToString(e.Value)
	case *ast.InterfaceType:
		if e.Methods == nil || len(e.Methods.List) == 0 {
			return "interface{}"
		}
		return "interface{...}"
	case *ast.FuncType:
		var params []string
		if e.Params != nil {
			for _, p := range e.Params.List {
				params = append(params, exprToString(p.Type))
			}
		}
		var results []string
		if e.Results != nil {
			for _, r := range e.Results.List {
				results = append(results, exprToString(r.Type))
			}
		}
		sig := "func(" + strings.Join(params, ", ") + ")"
		if len(results) == 1 {
			sig += " " + results[0]
		} else if len(results) > 1 {
			sig += " (" + strings.Join(results, ", ") + ")"
		}
		return sig
	case *ast.Ellipsis:
		return "..." + exprToString(e.Elt)
	case *ast.ChanType:
		switch e.Dir {
		case ast.SEND:
			return "chan<- " + exprToString(e.Value)
		case ast.RECV:
			return "<-chan " + exprToString(e.Value)
		default:
			return "chan " + exprToString(e.Value)
		}
	case *ast.ParenExpr:
		return "(" + exprToString(e.X) + ")"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

func funcSigsEqual(a, b FuncSig) bool {
	if a.Receiver != b.Receiver {
		return false
	}
	if len(a.Params) != len(b.Params) {
		return false
	}
	for i := range a.Params {
		if a.Params[i] != b.Params[i] {
			return false
		}
	}
	if len(a.Returns) != len(b.Returns) {
		return false
	}
	for i := range a.Returns {
		if a.Returns[i] != b.Returns[i] {
			return false
		}
	}
	return true
}

func formatFuncSig(f FuncSig) string {
	var sb strings.Builder
	sb.WriteString("func ")
	if f.Receiver != "" {
		sb.WriteString("(" + f.Receiver + ") ")
	}
	sb.WriteString(f.Name + "(")
	sb.WriteString(strings.Join(f.Params, ", "))
	sb.WriteString(")")
	if len(f.Returns) == 1 {
		sb.WriteString(" " + f.Returns[0])
	} else if len(f.Returns) > 1 {
		sb.WriteString(" (" + strings.Join(f.Returns, ", ") + ")")
	}
	return sb.String()
}

func formatInterfaceMethods(i InterfaceSig) string {
	var methods []string
	for _, m := range i.Methods {
		methods = append(methods, formatFuncSig(m))
	}
	return strings.Join(methods, "; ")
}
