package imagegen

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHash_Deterministic(t *testing.T) {
	t.Parallel()
	a := Hash("a creature, microscopic")
	b := Hash("a creature, microscopic")
	if a != b {
		t.Errorf("Hash not deterministic")
	}
	if len(a) != 64 {
		t.Errorf("Hash length = %d, want 64 hex chars", len(a))
	}
}

func TestCache_PutGet(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c := NewCache(dir)
	if err := c.Put("alpha", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := c.Get("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("Get = %q, want hello", got)
	}
}

func TestCache_GetMissReturnsNotExist(t *testing.T) {
	t.Parallel()
	c := NewCache(t.TempDir())
	_, err := c.Get("never written")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want ErrNotExist", err)
	}
}

func TestCache_GetByHash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c := NewCache(dir)
	if err := c.Put("alpha", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := c.GetByHash(Hash("alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("GetByHash = %q", got)
	}
}

func TestCache_CreatesDirOnPut(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "deeply", "nested")
	c := NewCache(dir)
	if err := c.Put("x", []byte{0xff}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("dir not created: %v", err)
	}
}
