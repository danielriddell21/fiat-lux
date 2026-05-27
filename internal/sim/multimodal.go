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

	// Find the most recent matching EventCreate.
	events := s.World.Events()
	var target *world.Event
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.ID <= sinceEvent {
			break
		}
		if ev.Kind != world.EventCreate {
			continue
		}
		if ev.Agent != by {
			continue
		}
		target = &events[i]
		break
	}
	if target == nil {
		return
	}
	if len(target.Props) < minProps {
		return
	}

	prompt := pb(target.TypeLabel, target.Props)
	entityID := target.EntityID
	cache := mm.Cache
	gen := mm.Generator

	s.imageWG.Add(1)
	go func() {
		defer s.imageWG.Done()
		// Cache hit: skip the API call.
		if cache != nil {
			if _, err := cache.Get(prompt); err == nil {
				s.attachImageURL(entityID, urlBase+imagegen.Hash(prompt)+".png")
				return
			} else if !errors.Is(err, os.ErrNotExist) {
				// Permissions error or similar; treat as cache miss.
				_ = err
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		data, _, err := gen.Generate(ctx, prompt)
		if err != nil || len(data) == 0 {
			return
		}
		if cache != nil {
			if err := cache.Put(prompt, data); err != nil {
				return
			}
		}
		s.attachImageURL(entityID, urlBase+imagegen.Hash(prompt)+".png")
	}()
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
