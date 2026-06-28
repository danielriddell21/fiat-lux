package sim

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/imagegen"
	"github.com/danielriddell21/fiat-lux/internal/world"
)

// maybeGenerateImage finds the most recent EventCreate authored by
// the given agent and (if multimodal is enabled and the entity's
// property count clears the threshold) kicks off an async image
// generation. The follow-up world.Modify writes image_url back onto
// the entity; replay reads the URL from the modified state without
// re-rendering.
//
// sinceEvent is the agent's previous high-water mark so we don't
// pick up stale Creates from prior ticks.
func (s *Sim) maybeGenerateImage(by world.EntityID, sinceEvent world.EventID) {
	if s.multimodal == nil || s.multimodal.Generator == nil {
		return
	}
	mm := s.multimodal
	minProps := mm.MinPropsCount
	if minProps <= 0 {
		minProps = 1
	}
	urlBase := mm.URLBase
	if urlBase == "" {
		urlBase = "/api/image/"
	}
	pb := mm.PromptBuilder
	if pb == nil {
		pb = imagegen.DefaultPromptBuilder
	}

	target := findRecentCreate(s.World.Events(), by, sinceEvent)
	if target == nil || len(target.Props) < minProps {
		return
	}

	prompt := pb(target.TypeLabel, target.Props)
	entityID := target.EntityID
	url := urlBase + imagegen.Hash(prompt) + ".png"

	s.imageWG.Add(1)
	go func() {
		defer s.imageWG.Done()
		if s.renderImage(mm.Cache, mm.Generator, prompt) {
			s.attachImageURL(entityID, url)
		}
	}()
}

// findRecentCreate returns the most recent EventCreate authored by the
// given agent with an ID past sinceEvent, or nil if there is none.
func findRecentCreate(events []world.Event, by world.EntityID, sinceEvent world.EventID) *world.Event {
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.ID <= sinceEvent {
			return nil
		}
		if ev.Kind == world.EventCreate && ev.Agent == by {
			return &events[i]
		}
	}
	return nil
}

// renderImage resolves the image bytes for prompt, returning true when
// a URL should be attached. A cache hit short-circuits the API call; a
// cache miss generates and (best-effort) caches the result.
func (s *Sim) renderImage(cache *imagegen.Cache, gen imagegen.Generator, prompt string) bool {
	if cache != nil {
		if _, err := cache.Get(prompt); err == nil {
			return true
		} else if !errors.Is(err, os.ErrNotExist) {
			// Permissions error or similar; treat as cache miss.
			_ = err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	data, _, err := gen.Generate(ctx, prompt)
	if err != nil || len(data) == 0 {
		return false
	}
	if cache != nil {
		if err := cache.Put(prompt, data); err != nil {
			return false
		}
	}
	return true
}

// attachImageURL patches the entity's properties with the image URL.
// The follow-up EventModify is what survives replay.
func (s *Sim) attachImageURL(id world.EntityID, url string) {
	patch := world.Properties{"image_url": url}
	if err := s.World.Modify(world.NoAgent, id, patch); err != nil {
		// Entity may have been destroyed in the interim; non-fatal.
		_ = err
	}
}

// WaitForImages blocks until all in-flight image goroutines have
// finished. Useful for tests; production paths can rely on Close.
func (s *Sim) WaitForImages() { s.imageWG.Wait() }
