package imagegen

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Cache memoises Generate calls by prompt hash so a deterministic
// world regenerates without re-hitting the provider.
type Cache struct {
	dir string
	mu  sync.Mutex
}

// NewCache creates a Cache rooted at dir. The directory is created
// on first write; absent permission to do so, writes return an error
// but lookups still succeed against any pre-populated content.
func NewCache(dir string) *Cache {
	return &Cache{dir: dir}
}

// Hash returns the deterministic key for the given prompt. Exposed
// so callers can construct URLs ("/api/image/<hash>.png") without
// re-running the generator.
func Hash(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

// Get returns the cached bytes for prompt or os.ErrNotExist. The
// returned slice is owned by the caller.
func (c *Cache) Get(prompt string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	path := c.path(prompt)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("imagegen: read %s: %w", path, err)
	}
	return data, nil
}

// GetByHash returns the bytes addressed by the given hash, used by
// the web API to serve /api/image/<hash>.png.
func (c *Cache) GetByHash(hash string) ([]byte, error) {
	if hash == "" {
		return nil, errors.New("imagegen: empty hash")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	path := filepath.Join(c.dir, hash+".bin")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("imagegen: read %s: %w", path, err)
	}
	return data, nil
}

// Put stores the bytes under the prompt's hash. Calls to Get with
// the same prompt subsequently return the stored bytes.
func (c *Cache) Put(prompt string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(c.dir, 0o750); err != nil {
		return fmt.Errorf("imagegen: mkdir %s: %w", c.dir, err)
	}
	path := c.path(prompt)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("imagegen: write %s: %w", path, err)
	}
	return nil
}

func (c *Cache) path(prompt string) string {
	return filepath.Join(c.dir, Hash(prompt)+".bin")
}

// Dir returns the on-disk cache directory.
func (c *Cache) Dir() string { return c.dir }
