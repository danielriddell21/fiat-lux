package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunPrintsBanner(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := run(nil, &buf); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	got := buf.String()
	for _, want := range []string{"fiat-lux", "let there be light", "phase 0"} {
		if !strings.Contains(got, want) {
			t.Errorf("banner output missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func TestRunVersionFlag(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := run([]string{"-version"}, &buf); err != nil {
		t.Fatalf("run -version returned error: %v", err)
	}

	if got := strings.TrimSpace(buf.String()); got != version {
		t.Errorf("version output: got %q, want %q", got, version)
	}
}
