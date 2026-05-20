package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/world"
)

// Saver is the narrow interface RunAutosave needs from a Store; it
// matches *Store but stays small for testing.
type Saver interface {
	Save(ctx context.Context, w *world.World) error
}

// SaveMode is the parsed form of a --save-mode flag.
type SaveMode struct {
	// Kind is "manual" (no autosave) or "interval" (periodic).
	Kind string
	// Interval is set when Kind == "interval".
	Interval time.Duration
}

// ParseSaveMode parses the user-facing string form:
//
//	"manual"           - no autosave (default; same as today)
//	"interval:<dur>"   - periodic save every duration, e.g. interval:30s
//	""                 - treated as "manual"
//
// Returns an error for any other form.
func ParseSaveMode(s string) (SaveMode, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "manual" {
		return SaveMode{Kind: "manual"}, nil
	}
	if rest, ok := strings.CutPrefix(s, "interval:"); ok {
		d, err := time.ParseDuration(strings.TrimSpace(rest))
		if err != nil {
			return SaveMode{}, fmt.Errorf("store: parse save-mode interval %q: %w", rest, err)
		}
		if d <= 0 {
			return SaveMode{}, fmt.Errorf("store: save-mode interval must be positive (got %s)", d)
		}
		return SaveMode{Kind: "interval", Interval: d}, nil
	}
	return SaveMode{}, fmt.Errorf("store: save-mode %q not recognised (try manual | interval:<duration>)", s)
}

// RunAutosave blocks until ctx is cancelled, periodically calling
// store.Save(world). It skips a tick if the world has not gained any
// new events since the previous save - a cheap way to avoid
// pointless writes on an idle sim. Save errors are reported through
// onError when non-nil; they do not stop the loop.
func RunAutosave(ctx context.Context, store Saver, w *world.World, interval time.Duration, onError func(error)) error {
	if store == nil {
		return fmt.Errorf("store: RunAutosave needs a non-nil store")
	}
	if w == nil {
		return fmt.Errorf("store: RunAutosave needs a non-nil world")
	}
	if interval <= 0 {
		return fmt.Errorf("store: RunAutosave interval must be positive (got %s)", interval)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastSavedEvents int
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			n := len(w.Events())
			if n == lastSavedEvents {
				continue // nothing new to persist
			}
			if err := store.Save(ctx, w); err != nil {
				if onError != nil {
					onError(err)
				}
				continue
			}
			lastSavedEvents = n
		}
	}
}
