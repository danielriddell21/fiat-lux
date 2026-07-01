package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/danielriddell21/fiat-lux/internal/sim"
)

const testVersion = "dev"

func TestRunPrintsBanner(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := run(nil, &buf, io.Discard, testVersion); err != nil {
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

	var buf bytes.Buffer
	if err := run([]string{"--version"}, &buf, io.Discard, testVersion); err != nil {
		t.Fatalf("run --version returned error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != testVersion {
		t.Errorf("--version output: got %q, want %q", got, testVersion)
	}
}

func TestRunHelp(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := run([]string{"help"}, &buf, io.Discard, testVersion); err != nil {
		t.Fatalf("run help: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"Usage", "fiatlux run", "fiatlux serve", "--world", "--db", "--save-mode", "--web-addr", "libsql://"} {
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
		{spec: "", wantNil: false},       // zero
		{spec: "zero", wantNil: false},   // zero
		{spec: "hash", wantNil: false},   // hash
		{spec: "ollama", wantNil: false}, // ollama with default model
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
	err := run([]string{"flibbertigibbet"}, &buf, io.Discard, testVersion)
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

func TestCmdStep_JSON(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := run([]string{"step", "--count", "8", "--brain", "stub:42", "--json"}, &out, io.Discard, testVersion)
	if err != nil {
		t.Fatalf("step --json: %v", err)
	}
	var summary stepSummary
	if err := json.Unmarshal(out.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary: %v\n--- got ---\n%s", err, out.String())
	}
	if summary.World != "kosmos" {
		t.Errorf("World = %q, want kosmos", summary.World)
	}
	if summary.Steps != 8 {
		t.Errorf("Steps = %d, want 8", summary.Steps)
	}
	if summary.EntitiesAlive == 0 {
		t.Errorf("EntitiesAlive = 0; stub brain should have created entities")
	}
	if summary.Ticks == 0 {
		t.Errorf("Ticks = 0; expected at least one tick advance")
	}
}

func TestCmdStep_Human(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	err := run([]string{"step", "--count", "4", "--brain", "stub:7"}, &out, io.Discard, testVersion)
	if err != nil {
		t.Fatalf("step (human): %v", err)
	}
	body := out.String()
	for _, want := range []string{"world:", "ticks:", "steps:", "entities_alive:", "tool_counts:"} {
		if !strings.Contains(body, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, body)
		}
	}
}

func TestCmdStep_RejectsZeroCount(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	err := run([]string{"step", "--count", "0"}, io.Discard, &stderr, testVersion)
	if err == nil {
		t.Errorf("expected error for --count 0")
	}
}

func TestCmdServe_BootsWebServer(t *testing.T) {
	// Not parallel: cmdServe registers a SIGINT handler via
	// signal.NotifyContext, and we send SIGINT to ourselves to trigger
	// graceful shutdown. Running this in parallel with another test
	// doing the same could lead to flakes.

	addr, err := pickFreeAddr()
	if err != nil {
		t.Fatalf("pickFreeAddr: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- run([]string{
			"serve",
			"--brain", "stub:42",
			"--tick", "50ms",
			"--web-addr", addr,
		}, io.Discard, io.Discard, testVersion)
	}()

	deadline := time.Now().Add(5 * time.Second)
	url := "http://" + addr + "/api/state"
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(50 * time.Millisecond)
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			lastErr = nil
			break
		}
		lastErr = fmt.Errorf("status %d", resp.StatusCode)
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr != nil {
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)
		<-done
		t.Fatalf("web server never came up: %v", lastErr)
	}

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("send SIGINT: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cmdServe returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cmdServe did not exit within 5s of SIGINT")
	}
}

func pickFreeAddr() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		return "", err
	}
	return addr, nil
}
