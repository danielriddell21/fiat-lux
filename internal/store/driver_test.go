package store

import "testing"

func TestResolveDSN(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input      string
		wantDriver string
		wantDSN    string
		wantErr    bool
	}{
		{input: "./kosmos.db", wantDriver: driverSQLite, wantDSN: "./kosmos.db"},
		{input: "/tmp/foo.db", wantDriver: driverSQLite, wantDSN: "/tmp/foo.db"},
		{input: ":memory:", wantDriver: driverSQLite, wantDSN: ":memory:"},
		{input: "file:./foo.db?_journal_mode=WAL", wantDriver: driverSQLite, wantDSN: "file:./foo.db?_journal_mode=WAL"},
		{input: "sqlite://./foo.db", wantDriver: driverSQLite, wantDSN: "./foo.db"},
		{input: "libsql://example.turso.io?authToken=xyz", wantDriver: driverLibSQL, wantDSN: "libsql://example.turso.io?authToken=xyz"},
		{input: "http://localhost:8080", wantDriver: driverLibSQL, wantDSN: "http://localhost:8080"},
		{input: "https://my-sqld.example.com", wantDriver: driverLibSQL, wantDSN: "https://my-sqld.example.com"},
		{input: "ws://localhost:8080", wantDriver: driverLibSQL, wantDSN: "ws://localhost:8080"},
		{input: "wss://example.turso.io?authToken=xyz", wantDriver: driverLibSQL, wantDSN: "wss://example.turso.io?authToken=xyz"},
		// Case-insensitive scheme matching.
		{input: "HTTPS://example.com", wantDriver: driverLibSQL, wantDSN: "HTTPS://example.com"},
		// Empty rejected.
		{input: "", wantErr: true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.input, func(t *testing.T) {
			t.Parallel()
			gotDriver, gotDSN, err := resolveDSN(c.input)
			if c.wantErr {
				if err == nil {
					t.Errorf("resolveDSN(%q): expected error, got driver=%q dsn=%q", c.input, gotDriver, gotDSN)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveDSN(%q): unexpected error: %v", c.input, err)
			}
			if gotDriver != c.wantDriver {
				t.Errorf("driver = %q, want %q", gotDriver, c.wantDriver)
			}
			if gotDSN != c.wantDSN {
				t.Errorf("dsn = %q, want %q", gotDSN, c.wantDSN)
			}
		})
	}
}

func TestParseSaveMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input    string
		wantKind string
		wantErr  bool
	}{
		{input: "", wantKind: "manual"},
		{input: "manual", wantKind: "manual"},
		{input: "interval:30s", wantKind: "interval"},
		{input: "interval:1m", wantKind: "interval"},
		{input: "interval: 5s", wantKind: "interval"}, // tolerate space
		{input: "interval:0s", wantErr: true},         // non-positive
		{input: "interval:bogus", wantErr: true},
		{input: "always", wantErr: true},
		{input: "interval", wantErr: true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseSaveMode(c.input)
			if c.wantErr {
				if err == nil {
					t.Errorf("ParseSaveMode(%q): expected error, got %+v", c.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSaveMode(%q): unexpected error: %v", c.input, err)
			}
			if got.Kind != c.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, c.wantKind)
			}
			if c.wantKind == "interval" && got.Interval <= 0 {
				t.Errorf("Interval = %s, want positive", got.Interval)
			}
		})
	}
}
