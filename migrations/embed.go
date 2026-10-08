// Package migrations embeds the SQL schema migrations applied by internal/database.
package migrations

import "embed"

// FS holds the numbered *.sql migration files.
//
//go:embed *.sql
var FS embed.FS
