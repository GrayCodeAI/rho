package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/GrayCodeAI/rho/internal/storage"
)

// Cache stores LLM responses keyed by (model, prompt_hash, params) to avoid re-calling APIs.
type Cache struct {
	Dir string
}

// CacheEntry is a single cached LLM response.
type CacheEntry struct {
	Model    string  `json:"model"`
	Prompt   string  `json:"prompt"`
	Response string  `json:"response"`
	Tokens   int     `json:"tokens"`
	CostUSD  float64 `json:"cost_usd"`
}

// DefaultCache returns a cache in Rho's user cache directory.
func DefaultCache() *Cache {
	return &Cache{Dir: filepath.Join(storage.CacheDir(), "eval", "cache")}
}

// Key computes a cache key from model and prompt.
func (c *Cache) Key(model, prompt string) string {
	h := sha256.Sum256([]byte(model + "\x00" + prompt))
	return hex.EncodeToString(h[:16])
}

// Get retrieves a cached response. Returns nil if not found.
func (c *Cache) Get(model, prompt string) *CacheEntry {
	path := filepath.Join(c.Dir, c.Key(model, prompt)+".json")
	data, err := os.ReadFile(path) // #nosec G304 -- path is built from a sha256 hash key joined with the local cache directory, not external input
	if err != nil {
		return nil
	}
	var entry CacheEntry
	if json.Unmarshal(data, &entry) != nil {
		return nil
	}
	return &entry
}

// Put stores a response in the cache.
func (c *Cache) Put(model, prompt, response string, tokens int, cost float64) error {
	if err := os.MkdirAll(c.Dir, 0o750); err != nil {
		return err
	}
	entry := CacheEntry{
		Model:    model,
		Prompt:   prompt,
		Response: response,
		Tokens:   tokens,
		CostUSD:  cost,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	path := filepath.Join(c.Dir, c.Key(model, prompt)+".json")
	return os.WriteFile(path, data, 0o600)
}

// Clear removes all cached entries.
func (c *Cache) Clear() error {
	return os.RemoveAll(c.Dir)
}
