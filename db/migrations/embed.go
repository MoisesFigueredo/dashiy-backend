package migrations

import "embed"

// Files contains every SQL migration bundled with the binary.
//
//go:embed *.sql
var Files embed.FS
