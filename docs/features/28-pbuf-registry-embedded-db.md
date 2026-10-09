---
uuid: "0cd2bfad-f505-53ef-9da1-5bc5874408ee"
kind: "feature"
product_uuid: "aa14cb8f-ba36-5afe-abc9-40d26b422f89"
portal_url: "https://joel.holmes.haus/discovery/0cd2bfad-f505-53ef-9da1-5bc5874408ee"
file: "docs/features/28-pbuf-registry-embedded-db.md"
body_sha256: "bcffcfaa5967eb1187c1b76275fc91bc84edb7adbcbe750a3e5573c50fdf2762"
product: joel.holmes.haus
type: feature
status: draft
source: "narwhal-catalog:joel.holmes.haus/features/28-pbuf-registry-embedded-db.md"
parent: "https://github.com/holmes89/narwhal/blob/main/designs/joel.holmes.haus/system.md"
---
# Feature 28: pbuf-registry — SQLite Embedded Backend

## Goal

Replace the PostgreSQL dependency in the forked `pbuf-registry` with an embedded SQLite database using `modernc.org/sqlite` (pure Go, no CGO). The result is a single binary with no external service dependencies, suitable for local development and small team use without running a Postgres instance.

---

## Background

`pbuf-registry` is the only viable self-hosted open-source protobuf schema registry. It was forked to `~/projects/pbuf-registry` to allow patching. In its current form it requires PostgreSQL + Redis (Redis optional), which is significant infrastructure overhead for a local dev tool.

The goal is to make a local registry as easy to run as `./pbuf-registry serve` — one binary, one file on disk.

### Why SQLite, not BoltDB/Badger/Pebble

pbuf-registry's data model is relational: modules → versions → files, users → tokens, modules → dependencies, ACL entries. Queries like "list all tags for module X" and "get transitive dependencies for tag Y" are joins across three or four tables. BoltDB, Badger, and Pebble are key-value stores — all join logic would have to be reimplemented in Go code, effectively rewriting the data layer from scratch. SQLite keeps the existing SQL interface intact; the porting work is translating PostgreSQL-specific syntax, not rearchitecting the schema.

`modernc.org/sqlite` is the right SQLite binding because it is pure Go (no C toolchain, no CGO), cross-compiles cleanly, and produces a single self-contained binary. The 10-20% read performance overhead vs the CGO binding is irrelevant for a schema registry — it is not a high-throughput system.

### Alternatives considered

| Option | Verdict |
|---|---|
| BoltDB (bbolt) | Rejected — key-value model requires rewriting all joins in Go; 3.5x storage bloat |
| Badger | Rejected — maintenance uncertainty, key-value limitations, memory-map overhead |
| Pebble | Rejected — same key-value limitations as Badger, and LSM overhead for small datasets |
| DuckDB | Rejected — OLAP engine, wrong fit for transactional metadata |
| mattn/go-sqlite3 | Rejected — CGO dependency breaks single-binary and cross-compilation goals |
| buf BSR on-prem | Rejected — paid license, Kubernetes required |

---

## Current Architecture

```
pbuf-registry binary
    │
    ├── internal/data/
    │   ├── RegistryRepository   (modules, tags, protofiles, dependencies, draft_tags)
    │   ├── MetadataRepository   (proto_parsed_data, tag_meta)
    │   ├── UserRepository       (users)
    │   ├── ACLRepository        (acl)
    │   └── DriftRepository      (drift_events, content_hash on protofiles)
    │
    ├── internal/server/         (gRPC + REST handlers, inject repository interfaces)
    ├── internal/background/     (proto parsing daemon, compaction daemon)
    ├── migrations/              (Goose SQL migrations — 9 files)
    │
    └── requires: PostgreSQL (pgx/v5), pgcrypto extension
```

All five repository types are **interfaces** — the business logic in `internal/server/` and `internal/background/` depends only on the interface, not the implementation. This makes swapping backends clean.

---

## Design

### New backend: `internal/data/sqlite/`

Create a parallel `internal/data/sqlite/` package implementing all five interfaces using `database/sql` + `modernc.org/sqlite`. The PostgreSQL implementations in `internal/data/` remain (config selects which backend is wired up at startup).

```
internal/data/
    registry.go          ← existing PostgreSQL RegistryRepository (keep)
    metadata.go          ← existing PostgreSQL MetadataRepository (keep)
    users.go             ← existing PostgreSQL UserRepository (keep)
    acl.go               ← existing PostgreSQL ACLRepository (keep)
    drift.go             ← existing PostgreSQL DriftRepository (keep)
    sqlite/
        registry.go      ← new SQLite RegistryRepository
        metadata.go      ← new SQLite MetadataRepository
        users.go         ← new SQLite UserRepository
        acl.go           ← new SQLite ACLRepository
        drift.go         ← new SQLite DriftRepository
        db.go            ← open/init helper, registers modernc driver
migrations/
    sqlite/              ← parallel migration files for SQLite dialect
        *.sql
```

`cmd/main.go` reads `config.Cfg.Data.Database.Driver` (`"postgres"` or `"sqlite"`) and initializes the corresponding set of repositories. When `"sqlite"`, it opens a `database/sql` connection to a local file path.

### Schema migration strategy

Keep Goose as the migration runner — it supports `database/sql` drivers. Create a parallel `migrations/sqlite/` directory with SQLite-dialect versions of all 9 migrations. The `migration.go` embedder gets a second embed for the SQLite set.

```go
//go:embed migrations/postgres/*.sql
var postgresMigrations embed.FS

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS
```

---

## PostgreSQL → SQLite Porting Map

| PostgreSQL feature | SQLite equivalent | Notes |
|---|---|---|
| `UUID PRIMARY KEY DEFAULT gen_random_uuid()` | `TEXT PRIMARY KEY` | Generate UUID in Go with `github.com/google/uuid` before INSERT |
| `$1, $2, ...` placeholders | `?, ?, ...` | Swap in all query strings |
| `TIMESTAMP WITH TIME ZONE` | `TEXT` | Store as RFC3339 string; parse in Go |
| `TIMESTAMP NOT NULL DEFAULT NOW()` | `TEXT NOT NULL DEFAULT (datetime('now'))` | SQLite datetime function |
| `JSONB` columns | `TEXT` | `json.Marshal` before write, `json.Unmarshal` after read |
| `pgcrypto` / `crypt()` / `gen_salt('bf')` | Drop entirely | Hash tokens with `golang.org/x/crypto/bcrypt` in Go before INSERT; compare with `bcrypt.CompareHashAndPassword` |
| `BOOLEAN NOT NULL DEFAULT FALSE` | `INTEGER NOT NULL DEFAULT 0` | SQLite stores booleans as integers |
| `VARCHAR(n)` | `TEXT` | SQLite ignores length limits |
| `ON CONFLICT ... DO UPDATE` | Same syntax (SQLite 3.24+) | modernc bundles SQLite 3.41+ — no change needed |
| `ON DELETE CASCADE` | Same syntax | Must enable `PRAGMA foreign_keys = ON` at connection open |
| Partial index (`WHERE content_hash IS NULL`) | Same syntax | SQLite supports WHERE in CREATE INDEX |
| `COALESCE()` in unique index expression | Replace NULLs with `''` | SQLite does not allow function calls in index expressions; store `''` instead of NULL for `previous_hash`/`current_hash` in drift_events |

### SQLite DDL — condensed

All tables translate almost verbatim. Key differences:

```sql
-- modules
CREATE TABLE IF NOT EXISTS modules (
    id TEXT PRIMARY KEY,           -- UUID generated in Go
    name TEXT UNIQUE NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- tags
CREATE TABLE IF NOT EXISTS tags (
    id TEXT PRIMARY KEY,
    module_id TEXT NOT NULL,
    tag TEXT NOT NULL,
    is_processed INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (module_id, tag)
);

-- protofiles
CREATE TABLE IF NOT EXISTS protofiles (
    id TEXT PRIMARY KEY,
    tag_id TEXT NOT NULL,
    filename TEXT NOT NULL,
    content TEXT NOT NULL,
    content_hash TEXT,             -- empty string instead of NULL for drift index
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (tag_id, filename)
);

-- draft_tags (JSONB → TEXT)
CREATE TABLE IF NOT EXISTS draft_tags (
    id TEXT PRIMARY KEY,
    module_id TEXT NOT NULL,
    tag TEXT NOT NULL,
    proto_files TEXT NOT NULL,     -- JSON encoded
    dependencies TEXT NOT NULL,   -- JSON encoded
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (module_id, tag)
);

-- proto_parsed_data
CREATE TABLE IF NOT EXISTS proto_parsed_data (
    id TEXT PRIMARY KEY,
    tag_id TEXT NOT NULL,
    filename TEXT NOT NULL,
    json TEXT NOT NULL,            -- was JSONB
    UNIQUE (tag_id, filename)
);

-- tag_meta
CREATE TABLE IF NOT EXISTS tag_meta (
    id TEXT PRIMARY KEY,
    tag_id TEXT NOT NULL,
    meta TEXT NOT NULL,            -- was JSONB
    UNIQUE (tag_id)
);

-- users (no pgcrypto; token column stores bcrypt hash)
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    token TEXT NOT NULL,           -- bcrypt hash, computed in Go
    type TEXT NOT NULL CHECK (type IN ('user', 'bot')),
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now'))
);

-- acl
CREATE TABLE IF NOT EXISTS acl (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    module_name TEXT NOT NULL,
    permission TEXT NOT NULL CHECK (permission IN ('read', 'write', 'admin')),
    created_at TEXT DEFAULT (datetime('now')),
    UNIQUE (user_id, module_name)
);

-- drift_events (COALESCE workaround: store '' not NULL for hashes)
CREATE TABLE IF NOT EXISTS drift_events (
    id TEXT PRIMARY KEY,
    module_id TEXT NOT NULL REFERENCES modules(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    filename TEXT NOT NULL,
    event_type TEXT NOT NULL CHECK (event_type IN ('added', 'modified', 'deleted')),
    previous_hash TEXT NOT NULL DEFAULT '',  -- '' instead of NULL
    current_hash TEXT NOT NULL DEFAULT '',   -- '' instead of NULL
    severity TEXT NOT NULL DEFAULT 'medium',
    detected_at TEXT DEFAULT (datetime('now')),
    acknowledged INTEGER NOT NULL DEFAULT 0
);

-- Unique drift index — no COALESCE needed since we use '' not NULL
CREATE UNIQUE INDEX IF NOT EXISTS idx_drift_events_unique
ON drift_events (tag_id, filename, event_type, previous_hash, current_hash);
```

---

## Implementation Phases

### Phase 1 — Repository layer
1. Add `modernc.org/sqlite` and `golang.org/x/crypto` to `go.mod`
2. Create `internal/data/sqlite/db.go` — open `database/sql`, enable `PRAGMA foreign_keys = ON`, register driver
3. Port `RegistryRepository` to `internal/data/sqlite/registry.go`
   - Swap placeholders (`$N` → `?`), UUID generation in Go, timestamp as RFC3339 string
4. Port remaining four repositories (`metadata`, `users`, `acl`, `drift`)
   - `users.go`: replace `crypt($1, gen_salt('bf'))` with `bcrypt.GenerateFromPassword`; replace `token = crypt($1, token)` comparison with `bcrypt.CompareHashAndPassword` — requires fetching the stored hash first then comparing in Go
5. Write `migrations/sqlite/*.sql` — SQLite-dialect versions of all 9 migrations

### Phase 2 — Startup wiring
6. Add `driver` field to config (`internal/config/config.go`) defaulting to `"postgres"` for backwards compat
7. Update `cmd/main.go` to branch on driver: initialize `database/sql` + SQLite repos OR existing pgxpool + Postgres repos
8. Update `migration.go` to select the correct embed set

### Phase 3 — Docker Compose & CI
9. Update `~/projects/beaver/docker-compose.yml` — drop `postgres` service; mount `./data/pbuf.db` into the registry container
10. Add a `Dockerfile.sqlite` (or a build tag variant) that produces a single self-contained binary (no CGO flags needed since modernc is pure Go)
11. Update GitHub Actions CI (`.github/workflows/`):
    - Remove any `services: postgres:` block from test jobs
    - Add a `go test ./internal/data/sqlite/...` step for the new backend
    - Add a build step that compiles the SQLite variant and uploads the binary as an artifact
    - Add a smoke test step: start the binary, push a test module via pbuf-cli, pull it back, assert success
12. Update `Makefile` (if present): add `make build-sqlite` target, `make test-sqlite` target

### Phase 4 — Validation
12. Manual smoke test: `./pbuf-registry serve --driver sqlite --db ./pbuf.db`
13. Push a module with `pbuf-cli`, pull it back, verify contents
14. Confirm drift detection still works (compute + store hashes, detect a changed file)
15. Confirm user/token auth round-trip (create user, authenticate, verify bcrypt comparison)

---

## Out of Scope

- Removing PostgreSQL support — it stays as a first-class option (`--driver postgres`)
- Performance benchmarking — SQLite is sufficient for local dev; no throughput requirements
- Read replication or multi-writer SQLite (WAL mode is fine for single-node)
- Full migration of the pbuf-cli to use buf CLI — separate effort

---

## Files Changed

| File | Change |
|---|---|
| `go.mod` | Add `modernc.org/sqlite`, `golang.org/x/crypto` |
| `internal/config/config.go` | Add `Driver string` field |
| `internal/data/sqlite/db.go` | New — open DB, PRAGMA setup |
| `internal/data/sqlite/registry.go` | New — SQLite RegistryRepository |
| `internal/data/sqlite/metadata.go` | New — SQLite MetadataRepository |
| `internal/data/sqlite/users.go` | New — SQLite UserRepository (bcrypt) |
| `internal/data/sqlite/acl.go` | New — SQLite ACLRepository |
| `internal/data/sqlite/drift.go` | New — SQLite DriftRepository |
| `migrations/sqlite/*.sql` | New — 9 SQLite-dialect migration files |
| `migration.go` | Branch on driver to select embed set |
| `cmd/main.go` | Branch on driver to initialize repositories |
| `~/projects/beaver/docker-compose.yml` | Drop postgres service; mount SQLite file |
| `.github/workflows/*.yml` | Remove postgres service dependency; add SQLite build + smoke test |
| `Makefile` | Add `build-sqlite` and `test-sqlite` targets |
