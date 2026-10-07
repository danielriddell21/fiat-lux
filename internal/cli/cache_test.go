package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveImageCacheDirPrefersTheFlag(t *testing.T) {
	if got := resolveImageCacheDir("/tmp/x"); got != "/tmp/x" {
		t.Errorf("resolveImageCacheDir(/tmp/x) = %q", got)
	}
}

func TestResolveImageCacheDirFallsBackToAPrivateTempDir(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only Linux derives the user cache directory from these variables")
	}
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")

	got := resolveImageCacheDir("")
	base := filepath.Dir(filepath.Dir(got))
	t.Cleanup(func() { _ = os.RemoveAll(base) })

	if !strings.HasPrefix(filepath.Base(base), "fiatlux-cache-") {
		t.Errorf("fallback %q is not a fresh fiatlux-cache- directory", got)
	}
	if info, err := os.Stat(base); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("fallback base %q: %v, want a private directory", base, err)
	}
}
