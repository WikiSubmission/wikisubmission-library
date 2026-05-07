-- ws-lib bootstrap for a shared Postgres instance (same pgvector container as ws-backend).
-- Run ONCE as a superuser (e.g. `postgres`). Idempotent — safe to re-run.
--
-- Usage (from host):
--   psql "postgres://postgres:SUPERPASS@HOST:5432/postgres" \
--        -v db_name=ws_lib_metadata \
--        -v db_user=ws_lib_backend \
--        -v db_pass='&xSR!fz4jyFeWSQ*gbF7?6j@' \
--        -f init.sql
--
-- Schema itself is created by the app at startup via db.DbSetup()
-- (see db/migrations/*.sql). This file only handles role + database +
-- extensions + permissions.

\set ON_ERROR_STOP on

-- 1. Role (login user for the app).
SELECT format('CREATE ROLE %I WITH LOGIN PASSWORD %L', :'db_user', :'db_pass')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = :'db_user')
\gexec

-- 2. Database owned by that role.
SELECT format('CREATE DATABASE %I OWNER %I', :'db_name', :'db_user')
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'db_name')
\gexec

-- 3. Switch into the new database as superuser to install extensions.
--    `pg_trgm` powers fuzzy search on s3_objects.file_key (see migrations/01_*.sql).
\c :"db_name"

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Optimized for fuzzy file-path matching.
ALTER DATABASE :"db_name" SET pg_trgm.similarity_threshold = 0.15;

-- 4. Permissions.
GRANT ALL ON SCHEMA public TO :"db_user";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES    TO :"db_user";
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO :"db_user";
