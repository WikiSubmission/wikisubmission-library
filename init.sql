-- ws-lib bootstrap for a shared Postgres instance (e.g. Coolify pgvector/pgvector:pg18).
-- Run ONCE as a superuser (e.g. `postgres`). Idempotent — safe to re-run.
--
-- Usage (from host):
--   psql "postgres://postgres:SUPERPASS@HOST:5432/postgres" \
--        -v db_name=ws_lib_metadata \
--        -v db_user=ws_lib_backend \
--        -v db_pass='tDte&458LdeCL7492IehdLRGiiu' \
--        -f init.sql
--
-- Or, for Coolify's built-in Postgres init (first-boot only), drop this file
-- into the container's /docker-entrypoint-initdb.d/ — but note it will NOT
-- re-run on subsequent boots, so prefer the manual invocation above.

\set ON_ERROR_STOP on

-- 1. Role (login user for the app). Can't use IF NOT EXISTS for CREATE ROLE,
--    so we gate it behind a lookup in pg_roles.
SELECT format('CREATE ROLE %I WITH LOGIN PASSWORD %L', :'db_user', :'db_pass')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'db_user')
\gexec

-- 2. Database owned by that role. CREATE DATABASE can't run inside a
--    transaction, so \gexec is used instead of a DO block.
SELECT format('CREATE DATABASE %I OWNER %I', :'db_name', :'db_user')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'db_name')
\gexec

-- 3. Switch into the new database as superuser to install extensions and
--    tune database-level GUCs.
\c :"db_name"

CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER DATABASE :"db_name" SET pg_trgm.similarity_threshold = 0.15;

-- 4. Permissions. Owner already has these, but being explicit makes the
--    script work even if the role was created separately or the DB owner
--    was changed later.
GRANT ALL ON SCHEMA public TO :"db_user";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES    TO :"db_user";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO :"db_user";
