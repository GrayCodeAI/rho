package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// WasmPluginRuntime runs WASM-compiled plugins using wazero (pure Go, no CGO).
// WASM plugins are faster than subprocess plugins (no process spawn overhead)
// and more secure (capability-restricted execution by default).
type WasmPluginRuntime struct {
	manifestPath string
	manifest     *Manifest
}

// NewWasmPluginRuntime creates a WASM plugin runtime from a manifest.
func NewWasmPluginRuntime(manifestPath string) (*WasmPluginRuntime, error) {
	m, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("wasm: load manifest: %w", err)
	}
	return &WasmPluginRuntime{
		manifestPath: manifestPath,
		manifest:     m,
	}, nil
}

// ExecuteTool runs a WASM plugin tool with the given input and returns the output.
func (w *WasmPluginRuntime) ExecuteTool(ctx context.Context, toolName string, input json.RawMessage) (string, error) {
	var cmd *CommandDef
	for i, c := range w.manifest.Commands {
		if c.Name == toolName {
			cmd = &w.manifest.Commands[i]
			break
		}
	}
	if cmd == nil {
		return "", fmt.Errorf("wasm plugin %q: unknown tool %q", w.manifest.Name, toolName)
	}

	// Resolve the WASM binary path relative to the manifest.
	wasmPath := cmd.Script
	if !filepath.IsAbs(wasmPath) {
		wasmPath = filepath.Join(filepath.Dir(w.manifestPath), wasmPath)
	}

	// Read the WASM binary.
	wasmBytes, err := os.ReadFile(wasmPath) // #nosec G304 -- wasmPath is resolved from the plugin's own manifest directory, trusted like other plugin config
	if err != nil {
		return "", fmt.Errorf("wasm plugin %q: read %s: %w", w.manifest.Name, wasmPath, err)
	}

	// Create a wazero runtime with a timeout.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rt := wazero.NewRuntime(ctx)
	defer func() { _ = rt.Close(ctx) }()

	// Enable WASI for basic I/O.
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	// Compile the WASM module.
	compiled, err := rt.CompileModule(ctx, wasmBytes)
	if err != nil {
		return "", fmt.Errorf("wasm plugin %q: compile: %w", w.manifest.Name, err)
	}
	defer func() { _ = compiled.Close(ctx) }()

	// Set up stdin/stdout.
	inputBytes, _ := json.Marshal(input)
	stdin := bytes.NewReader(inputBytes)
	var stdoutBuf bytes.Buffer

	// Instantiate the module with stdin/stdout configured.
	_, err = rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().
		WithName(toolName).
		WithStdin(stdin).
		WithStdout(&stdoutBuf).
		WithSysNanosleep().
		WithSysNanotime().
		WithSysWalltime())
	if err != nil {
		return "", fmt.Errorf("wasm plugin %q: execute %s: %w", w.manifest.Name, toolName, err)
	}

	return stdoutBuf.String(), nil
}

// Name returns the plugin name.
func (w *WasmPluginRuntime) Name() string {
	return w.manifest.Name
}
