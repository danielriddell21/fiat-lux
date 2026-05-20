package store

import (
	"errors"
	"strings"

	// Blank-import the libsql driver so it self-registers with
	// database/sql under the name "libsql". The pure-Go HTTP/WS
	// client supports libsql://, http(s)://, and ws(s):// URLs.
	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

// driverSQLite is the modernc.org/sqlite registration name.
const driverSQLite = "sqlite"

// driverLibSQL is the libsql-client-go registration name.
const driverLibSQL = "libsql"

// resolveDSN inspects the user-supplied --db value and returns the
// SQL driver name plus a DSN normalised for that driver. Recognised
// forms:
//
//	bare path / :memory: / file: URI / sqlite:// → sqlite
//	libsql://, http(s)://, ws(s):// → libsql
//
// An empty input is rejected; the caller is responsible for not
// invoking the store when --db is unset.
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
