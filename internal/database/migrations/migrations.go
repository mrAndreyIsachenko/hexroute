package migrations

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const postgresDirectory = "postgres"

var (
	//go:embed postgres/*.sql
	postgresFiles embed.FS

	migrationFilename = regexp.MustCompile(`^([0-9]{6})_([a-z0-9_]+)\.(up|down)\.sql$`)

	ErrInvalidMigrationSet = errors.New("invalid PostgreSQL migration set")
)

type Migration struct {
	Version    uint64
	Name       string
	Up         string
	Down       string
	UpChecksum string
}

type migrationPair struct {
	name string
	up   string
	down string
}

func PostgreSQL() ([]Migration, error) {
	entries, err := fs.ReadDir(postgresFiles, postgresDirectory)
	if err != nil {
		return nil, fmt.Errorf("%w: read embedded files", ErrInvalidMigrationSet)
	}

	pairs := make(map[uint64]migrationPair)
	for _, entry := range entries {
		if entry.IsDir() {
			return nil, fmt.Errorf("%w: unexpected directory", ErrInvalidMigrationSet)
		}
		match := migrationFilename.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil, fmt.Errorf("%w: unexpected filename", ErrInvalidMigrationSet)
		}
		version, parseErr := strconv.ParseUint(match[1], 10, 64)
		if parseErr != nil || version == 0 {
			return nil, fmt.Errorf("%w: invalid version", ErrInvalidMigrationSet)
		}
		content, readErr := postgresFiles.ReadFile(postgresDirectory + "/" + entry.Name())
		if readErr != nil || len(strings.TrimSpace(string(content))) == 0 {
			return nil, fmt.Errorf("%w: unreadable or empty migration", ErrInvalidMigrationSet)
		}

		pair := pairs[version]
		if pair.name != "" && pair.name != match[2] {
			return nil, fmt.Errorf("%w: mismatched migration pair", ErrInvalidMigrationSet)
		}
		pair.name = match[2]
		switch match[3] {
		case "up":
			if pair.up != "" {
				return nil, fmt.Errorf("%w: duplicate up migration", ErrInvalidMigrationSet)
			}
			pair.up = string(content)
		case "down":
			if pair.down != "" {
				return nil, fmt.Errorf("%w: duplicate down migration", ErrInvalidMigrationSet)
			}
			pair.down = string(content)
		default:
			return nil, fmt.Errorf("%w: invalid direction", ErrInvalidMigrationSet)
		}
		pairs[version] = pair
	}

	versions := make([]uint64, 0, len(pairs))
	for version := range pairs {
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })

	migrations := make([]Migration, 0, len(versions))
	for _, version := range versions {
		pair := pairs[version]
		if pair.name == "" || pair.up == "" || pair.down == "" {
			return nil, fmt.Errorf("%w: incomplete migration pair", ErrInvalidMigrationSet)
		}
		checksum := sha256.Sum256([]byte(pair.up))
		migrations = append(migrations, Migration{
			Version:    version,
			Name:       pair.name,
			Up:         pair.up,
			Down:       pair.down,
			UpChecksum: hex.EncodeToString(checksum[:]),
		})
	}
	if len(migrations) == 0 {
		return nil, fmt.Errorf("%w: no migrations", ErrInvalidMigrationSet)
	}
	return migrations, nil
}
