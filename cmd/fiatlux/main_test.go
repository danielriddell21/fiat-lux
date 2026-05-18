package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
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
	for _, want := range []string{"Usage", "fiatlux run", "--world", "--db"} {
		if !strings.Contains(got, want) {
			t.Errorf("help missing %q\n--- got ---\n%s", want, got)
		}
	}
}
