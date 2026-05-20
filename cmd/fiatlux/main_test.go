package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/danielriddell21/fiat-lux/internal/memory"
	"github.com/danielriddell21/fiat-lux/internal/sim"
)

func TestRunPrintsBanner(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := run(nil, &buf, io.Discard); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	got := buf.String()
	for _, want := range []string{"fiat-lux", "let there be light", "fiatlux run"} {
		if !strings.Contains(got, want) {
			t.Errorf("banner output missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func TestRunVersionFlag(t *testing.T) {
	t.Parallel()

	for _, arg := range []string{"-version", "--version", "version"} {
		var buf bytes.Buffer
		if err := run([]string{arg}, &buf, io.Discard); err != nil {
			t.Fatalf("run %s returned error: %v", arg, err)
		}
		if got := strings.TrimSpace(buf.String()); got != version {
			t.Errorf("%s output: got %q, want %q", arg, got, version)
		}
	}
}

func TestRunHelp(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := run([]string{"help"}, &buf, io.Discard); err != nil {
		t.Fatalf("run help: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"Usage", "fiatlux run", "--world", "--db", "--save-mode", "--web-addr", "libsql://"} {
		if !strings.Contains(got, want) {
			t.Errorf("help missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func TestBuildEmbedder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		spec    string
		wantErr bool
		wantNil bool
	}{
		{spec: "", wantNil: false},        // zero
		{spec: "zero", wantNil: false},    // zero
		{spec: "hash", wantNil: false},    // hash
		{spec: "ollama", wantNil: false},  // ollama with default model
		{spec: "ollama:nomic-embed-text", wantNil: false},
		{spec: "bogus", wantErr: true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.spec, func(t *testing.T) {
			t.Parallel()
			emb, err := buildEmbedder(c.spec)
			if c.wantErr {
				if err == nil {
					t.Errorf("buildEmbedder(%q): expected error, got %v", c.spec, emb)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildEmbedder(%q): %v", c.spec, err)
			}
			if (emb == nil) != c.wantNil {
				t.Errorf("buildEmbedder(%q): nil = %v, want %v", c.spec, emb == nil, c.wantNil)
			}
		})
	}
	// Compile-time assertion that buildEmbedder returns memory.Embedder.
	var _ func(string) (memory.Embedder, error) = buildEmbedder
}

func TestChainObservers(t *testing.T) {
	t.Parallel()
	var aCalled, bCalled int
	a := func(sim.StepResult) { aCalled++ }
	b := func(sim.StepResult) { bCalled++ }

	if chainObservers(nil, nil) != nil {
		t.Error("both nil should return nil")
	}

	chainObservers(a, nil)(sim.StepResult{})
	if aCalled != 1 || bCalled != 0 {
		t.Errorf("a only: a=%d b=%d", aCalled, bCalled)
	}
	aCalled = 0

	chainObservers(nil, b)(sim.StepResult{})
	if aCalled != 0 || bCalled != 1 {
		t.Errorf("b only: a=%d b=%d", aCalled, bCalled)
	}
	bCalled = 0

	chainObservers(a, b)(sim.StepResult{})
	if aCalled != 1 || bCalled != 1 {
		t.Errorf("both: a=%d b=%d", aCalled, bCalled)
	}
}

func TestRunUnknownSubcommand(t *testing.T) {
	t.Parallel()
	// Unknown subcommands fall through to the banner path, which
	// treats args[0] as a flag and errors on the flag parser.
	var buf bytes.Buffer
	err := run([]string{"flibbertigibbet"}, &buf, io.Discard)
	if err == nil {
		// Equally acceptable: the banner is printed with a warning.
		// Just assert *something* happened (no panic).
		if buf.Len() == 0 {
			t.Error("expected output or error for unknown subcommand")
		}
		return
	}
	_ = errors.Unwrap(err)
}
