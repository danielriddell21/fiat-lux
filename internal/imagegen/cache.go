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

type Cache struct {
	dir string
	mu  sync.Mutex
}

func NewCache(dir string) *Cache {
	return &Cache{dir: dir}
}

func Hash(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

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

func (c *Cache) Dir() string { return c.dir }
