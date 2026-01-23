-- 1. Connect to the correct database (Docker creates it, but we ensure we are on it)
\c ws_lib_metadata;

-- 2. Enable extensions
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- 3. Set global database parameters
ALTER DATABASE ws_lib_metadata
SET
    pg_trgm.similarity_threshold = 0.15;

-- 4. Fix permissions for PostgreSQL 15/16+ 
-- In newer versions, we must explicitly grant CREATE on the public schema to the user.
GRANT ALL ON SCHEMA public TO ws_lib_backend;

GRANT ALL PRIVILEGES ON DATABASE ws_lib_metadata TO ws_lib_backend;

-- 5. Ensure future objects (tables/sequences) created by migrations are manageable
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO ws_lib_backend;

ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO ws_lib_backend;