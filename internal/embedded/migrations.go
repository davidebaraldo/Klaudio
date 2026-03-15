package embedded

import "io/fs"

// migrationsFS holds the embedded SQL migration files.
var migrationsFS fs.FS

// RegisterMigrations stores the embedded migrations filesystem.
// Called once at startup from cmd/klaudio where the go:embed directive lives.
func RegisterMigrations(fsys fs.FS) {
	migrationsFS = fsys
}

// MigrationsFS returns the embedded migrations filesystem, or nil if not registered.
func MigrationsFS() fs.FS {
	return migrationsFS
}
