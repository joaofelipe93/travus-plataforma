// Package migrations embute as migrações SQL (goose) no binário da API.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
