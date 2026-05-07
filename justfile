# ws-lib justfile — run `just` to list recipes.

set dotenv-load := true
set positional-arguments := true
set shell := ["bash", "-euo", "pipefail", "-c"]

bin := "bin/server"
dumps_dir := "db/dumps"

# Default: list recipes
default:
    @just --list

# ---------------------------------------------------------------------------
# Dev
# ---------------------------------------------------------------------------

# Start the API in development mode
run:
    APP_ENV=development go run ./api/

# Compile a static binary to bin/server
build:
    @mkdir -p bin
    CGO_ENABLED=0 go build -trimpath -ldflags="-w -s" -o {{bin}} ./api/
    echo "Binary written to {{bin}}"

# Run all tests (DB tests require live DATABASE_* env vars)
test:
    go test ./...

# Tidy all Go modules in the workspace
tidy:
    cd api && go mod tidy
    cd db  && go mod tidy
    cd aws && go mod tidy
    go work sync

# Remove compiled binaries and Go build cache
clean:
    rm -rf bin/
    go clean -cache

# ---------------------------------------------------------------------------
# Connection helpers (private)
# ---------------------------------------------------------------------------

# Resolve a target name (local|coolify|hetzner|...) to a Postgres URL.
# Looks up DATABASE_URL_<UPPER>; for `local` falls back to composed DSN.
[private]
url TARGET:
    #!/usr/bin/env bash
    set -euo pipefail
    name="$1"
    var="DATABASE_URL_$(echo "$name" | tr '[:lower:]' '[:upper:]')"
    val="${!var:-}"
    if [ -n "$val" ]; then echo "$val"; exit 0; fi
    if [ "$name" = "local" ] && [ -n "${DATABASE_USER:-}" ]; then
      echo "host=${DATABASE_DOMAIN} port=${DATABASE_PORT} user=${DATABASE_USER} password=${DATABASE_PASSWORD} dbname=${DATABASE_NAME} sslmode=${DATABASE_SSL_MODE:-disable}"
      exit 0
    fi
    echo "ERROR: $var is not set in .env (and no fallback available for '$name')" >&2
    exit 1

# Build a `psql` invocation against a target.
[private]
psql TARGET *ARGS:
    #!/usr/bin/env bash
    set -euo pipefail
    url="$(just url "$1")"; shift
    psql "$url" --set ON_ERROR_STOP=1 "$@"

# ---------------------------------------------------------------------------
# DB lifecycle (local)
# ---------------------------------------------------------------------------

# Create local PostgreSQL role + database + grants
db-setup:
    #!/usr/bin/env bash
    set -euo pipefail
    case "$(uname -s)" in
      Darwin) admin="psql -d postgres --set ON_ERROR_STOP=1" ;;
      *)      admin="sudo -u postgres psql --set ON_ERROR_STOP=1" ;;
    esac
    $admin -c "CREATE USER ${DATABASE_USER} WITH PASSWORD '${DATABASE_PASSWORD}';" || echo "User exists."
    $admin -c "CREATE DATABASE ${DATABASE_NAME} OWNER ${DATABASE_USER};" || echo "Database exists."
    $admin -c "GRANT ALL PRIVILEGES ON DATABASE ${DATABASE_NAME} TO ${DATABASE_USER};"
    case "$(uname -s)" in
      Darwin) admin_db="psql -d ${DATABASE_NAME} --set ON_ERROR_STOP=1" ;;
      *)      admin_db="sudo -u postgres psql -d ${DATABASE_NAME} --set ON_ERROR_STOP=1" ;;
    esac
    $admin_db -c "CREATE EXTENSION IF NOT EXISTS pg_trgm;"
    $admin_db -c "ALTER DATABASE ${DATABASE_NAME} SET pg_trgm.similarity_threshold = 0.15;"
    PGPASSWORD="${DATABASE_PASSWORD}" psql -U "${DATABASE_USER}" -h "${DATABASE_DOMAIN}" -p "${DATABASE_PORT}" \
      -d "${DATABASE_NAME}" --set ON_ERROR_STOP=1 \
      -c "GRANT USAGE, CREATE ON SCHEMA public TO ${DATABASE_USER};"
    echo "Local DB ready."

# Drop local database and role
db-drop:
    #!/usr/bin/env bash
    set -euo pipefail
    case "$(uname -s)" in
      Darwin) admin="psql -d postgres --set ON_ERROR_STOP=1" ;;
      *)      admin="sudo -u postgres psql --set ON_ERROR_STOP=1" ;;
    esac
    $admin -c "DROP DATABASE IF EXISTS ${DATABASE_NAME};"
    $admin -c "DROP USER IF EXISTS ${DATABASE_USER};"

# Bootstrap a shared Postgres (Coolify/Hetzner) via init.sql.
# Same pgvector instance as ws-backend; this only adds the ws-lib role + DB.
coolify-init:
    @test -n "${POSTGRES_ADMIN_URL:-}" || (echo "ERROR: POSTGRES_ADMIN_URL is not set in .env" && exit 1)
    psql "$POSTGRES_ADMIN_URL" --set ON_ERROR_STOP=1 \
      -v db_name=$DATABASE_NAME -v db_user=$DATABASE_USER -v db_pass="'$DATABASE_PASSWORD'" \
      -f init.sql
    echo "Bootstrap complete for $DATABASE_NAME."

# ---------------------------------------------------------------------------
# Snapshots
# ---------------------------------------------------------------------------

# Dump data from a target into db/dumps/<ts>__<source>.dump (default: coolify)
export SOURCE="coolify":
    #!/usr/bin/env bash
    set -euo pipefail
    url="$(just url "$1")"
    mkdir -p {{dumps_dir}}
    ts="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
    out="{{dumps_dir}}/${ts}__$1.dump"
    echo "Exporting $1 -> $out"
    pg_dump --format=custom --data-only --no-owner --no-acl "$url" -f "$out"
    rows=$(psql "$url" -tAc "SELECT count(*) FROM s3_objects" 2>/dev/null || echo "?")
    sz=$(du -sh "$out" | cut -f1)
    echo "Done. dump=$sz, s3_objects=$rows rows."

# List dumps in db/dumps/, newest first
dumps:
    @ls -lh --time-style=long-iso {{dumps_dir}}/*.dump 2>/dev/null | awk '{print $6, $7, $5, $8}' || echo "no dumps yet"

# Restore a snapshot into local (default: latest dump)
import VERSION="latest":
    #!/usr/bin/env bash
    set -euo pipefail
    if [ "$1" = "latest" ]; then
      file=$(ls -1t {{dumps_dir}}/*.dump 2>/dev/null | head -1)
    else
      file=$(ls -1 {{dumps_dir}}/*$1*.dump 2>/dev/null | head -1)
    fi
    [ -n "$file" ] && [ -f "$file" ] || { echo "ERROR: no dump matching '$1'" >&2; exit 1; }
    url="$(just url local)"
    echo "Restoring $file into local..."
    psql "$url" --set ON_ERROR_STOP=1 -c "TRUNCATE TABLE s3_objects RESTART IDENTITY CASCADE;"
    pg_restore --data-only --single-transaction --no-owner -d "$url" "$file"
    psql "$url" --set ON_ERROR_STOP=1 -c "SELECT setval(pg_get_serial_sequence('s3_objects','id'), (SELECT MAX(id) FROM s3_objects));"
    echo "Import complete."

# Push a snapshot to a non-local target (coolify|hetzner|...). Prompts.
push TARGET VERSION="latest":
    #!/usr/bin/env bash
    set -euo pipefail
    target="$1"; ver="${2:-latest}"
    [ "$target" = "local" ] && { echo "Use 'just import' for local." >&2; exit 1; }
    if [ "$ver" = "latest" ]; then
      file=$(ls -1t {{dumps_dir}}/*.dump 2>/dev/null | head -1)
    else
      file=$(ls -1 {{dumps_dir}}/*$ver*.dump 2>/dev/null | head -1)
    fi
    [ -n "$file" ] && [ -f "$file" ] || { echo "ERROR: no dump matching '$ver'" >&2; exit 1; }
    url="$(just url "$target")"
    host=$(echo "$url" | sed -E 's#.*@([^/]+)/.*#\1#')
    echo "About to overwrite $target ($host) with $file."
    read -r -p "Continue? [y/N] " ans
    [ "$ans" = "y" ] || [ "$ans" = "Y" ] || { echo "Aborted."; exit 1; }
    psql "$url" --set ON_ERROR_STOP=1 -c "TRUNCATE TABLE s3_objects RESTART IDENTITY CASCADE;"
    pg_restore --data-only --single-transaction --no-owner -d "$url" "$file"
    psql "$url" --set ON_ERROR_STOP=1 -c "SELECT setval(pg_get_serial_sequence('s3_objects','id'), (SELECT MAX(id) FROM s3_objects));"
    echo "Pushed to $target."

# Compare row counts of s3_objects between two targets (sanity check post-migration)
verify A B:
    #!/usr/bin/env bash
    set -euo pipefail
    ua="$(just url "$1")"; ub="$(just url "$2")"
    printf "%-22s %12s %12s %s\n" "table" "$1" "$2" "delta"
    printf "%-22s %12s %12s %s\n" "----------------------" "------------" "------------" "-----"
    ca=$(psql "$ua" -tAc "SELECT count(*) FROM s3_objects" 2>/dev/null || echo "?")
    cb=$(psql "$ub" -tAc "SELECT count(*) FROM s3_objects" 2>/dev/null || echo "?")
    d=""
    if [ "$ca" != "$cb" ]; then d="MISMATCH"; fi
    printf "%-22s %12s %12s %s\n" "s3_objects" "$ca" "$cb" "$d"
    [ -z "$d" ] && echo "OK — counts match." || { echo "WARNING: mismatch."; exit 1; }

# ---------------------------------------------------------------------------
# Docker
# ---------------------------------------------------------------------------

# Build the production Docker image
docker-build:
    docker build -t ws-lib:latest .

# Run the container locally (reads .env for DB vars)
docker-run: docker-build
    docker run --rm -p 8080:8080 --env-file .env ws-lib:latest

# Bring up the full local docker-compose stack (Postgres + API + Prometheus + Grafana)
compose-up:
    docker compose up --build -d

# Tear down the docker-compose stack
compose-down:
    docker compose down
