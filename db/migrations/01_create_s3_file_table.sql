CREATE TABLE IF NOT EXISTS s3_objects (
    id SERIAL PRIMARY KEY,
    file_key TEXT UNIQUE NOT NULL,
    file_size BIGINT,
    last_modified TIMESTAMP WITH TIME ZONE,
    etag TEXT,
    indexed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Index for fuzzy searching on the file paths
CREATE INDEX IF NOT EXISTS idx_s3_objects_key_trgm ON s3_objects USING gin (file_key gin_trgm_ops);