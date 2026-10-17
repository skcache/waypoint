// Package migrations embeds Waypoint's ordered SQL migrations so the server
// binary, the migrate command, and the integration tests all apply the exact
// same schema without external tooling.
//
// Files are named NNNN_description.sql; the numeric prefix is the migration
// version. The migration runner records each applied version in
// schema_migrations together with a SHA-256 checksum, so a file that was
// edited after being applied aborts the next run instead of going unnoticed.
package migrations

import (
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed *.sql
var embedded embed.FS

// File is one embedded migration.
type File struct {
	Version int64
	Name    string
	SQL     string
}

// All returns every embedded migration ordered by version.
func All() ([]File, error) {
	entries, err := embedded.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	files := make([]File, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		version, err := parseVersion(name)
		if err != nil {
			return nil, err
		}
		body, err := embedded.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		files = append(files, File{Version: version, Name: name, SQL: string(body)})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Version < files[j].Version })

	for i, f := range files {
		if f.Version != int64(i+1) {
			return nil, fmt.Errorf("embedded migrations are not a contiguous 1..N sequence: %s is out of place", f.Name)
		}
	}
	return files, nil
}

// Latest returns the highest embedded migration version, or 0 when the
// embedded set is empty.
func Latest() (int64, error) {
	files, err := All()
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
	}
	return files[len(files)-1].Version, nil
}

func parseVersion(name string) (int64, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("migration %s: want NNNN_description.sql", name)
	}
	version, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("migration %s: invalid version prefix %q", name, prefix)
	}
	return version, nil
}
