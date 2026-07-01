package store

import (
	"errors"
	"strings"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

const driverSQLite = "sqlite"

const driverLibSQL = "libsql"

func resolveDSN(input string) (driver, dsn string, err error) {
	if input == "" {
		return "", "", errors.New("store: empty DSN")
	}
	lower := strings.ToLower(input)
	switch {
	case strings.HasPrefix(lower, "libsql://"),
		strings.HasPrefix(lower, "http://"),
		strings.HasPrefix(lower, "https://"),
		strings.HasPrefix(lower, "ws://"),
		strings.HasPrefix(lower, "wss://"):
		return driverLibSQL, input, nil
	case strings.HasPrefix(lower, "sqlite://"):
		// Strip the scheme: modernc.org/sqlite doesn't accept it.
		return driverSQLite, input[len("sqlite://"):], nil
	}
	// Bare paths, ":memory:", and "file:" URIs all route to sqlite.
	return driverSQLite, input, nil
}
