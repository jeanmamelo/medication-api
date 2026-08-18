// Package migrations embeds the SQL migrations into the migration binary.
package migrations

import "embed"

// FS contains all SQL migration files in this directory.
//
//go:embed *.sql
var FS embed.FS
