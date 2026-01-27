# WikiSubmission Library API

Source code repository for the backend services powering [wikisubmission.org](https://wikisubmission.org).

## Description

This project provides a high-performance API designed to index and serve files stored in Amazon S3. It maintains a local metadata index in PostgreSQL to enable fast, fuzzy search capabilities that are natively unavailable in S3.

## System Architecture

The system is designed for high-performance metadata retrieval using the following stack:
* **Backend**: Go (Gin Gonic) utilizing golang.org/x/sync/singleflight to prevent thundering herds on cache misses.
* **Database**: PostgreSQL with pg_trgm for fuzzy search and B-Tree indices for exact-path lookups.
* **CDN/Cache**: Cloudflare Edge caching for public assets and dynamic response deduplication.
* **Storage**: Amazon S3 with CloudFront Signed URL support for /private/ pathing.
* **Monitoring**: Prometheus metrics and Grafana dashboards.

---

## Database Initialization

The following SQL commands must be executed manually by a superuser to prepare the PostgreSQL environment before the application can start:

```sql
-- 1. Create the database
CREATE DATABASE ws_lib_metadata;

-- 2. Enable the Trigram extension
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- 3. Set similarity threshold (optimized for fuzzy matching)
ALTER DATABASE ws_lib_metadata SET pg_trgm.similarity_threshold = 0.15;

-- 4. Create the application service role
CREATE ROLE ws_lib_backend WITH LOGIN PASSWORD 'tDte&458LdeCL7492IehdLRGiiu';

-- 5. Grant necessary permissions
\c ws_lib_metadata;
GRANT CONNECT ON DATABASE ws_lib_metadata TO ws_lib_backend;
GRANT ALL ON SCHEMA public TO ws_lib_backend;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO ws_lib_backend;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO ws_lib_backend;
```

## Local Setup
1. **Prerequisites**
- Go 1.22+
- PostgreSQL 15+
- Docker & Docker Compose (optional)

2. **Clone the Repository**
```
git clone https://github.com/WikiSubmission/ws-lib.git
cd ws-lib
```

3. **Backend Configuration**
Create a .env file in the root directory based on the following template:
# Database Configuration
```
DATABASE_USER=ws_lib_backend
DATABASE_PASSWORD=your_password
DATABASE_DOMAIN=localhost
DATABASE_PORT=5432
DATABASE_NAME=ws_lib_metadata
DATABASE_SSL_MODE=disable
```
# AWS Configuration
```
AWS_REGION=us-east-1
BUCKET_NAME=wikisubmission
SQS_QUEUE_URL=https://sqs.us-east-1.amazonaws.com/
```
# CloudFront Configuration
```
CLOUDFRONT_BASE_URL=https://cdn.wikisubmission.org
CLOUDFRONT_PUBLIC_KEY_ID=KXXXXXXXXXXXX
CLOUDFRONT_PRIVATE_KEY_PATH=./aws/private_key.pem
```
4. Install Dependencies
`go work sync`
5. Launch the Application
`go run ./api`

# Deployment
The project includes a multi-stage Dockerfile and docker-compose.yaml for production deployments.
`docker-compose up --build -d`
# API Endpoints

The API is optimized for high performance using `singleflight` to coalesce concurrent requests, preventing database "thundering herds" during traffic spikes.

### 1. Landing Page
`GET /`
* **Description**: Serves the main landing page for the WikiSubmission Library.
* **Caching**: includes `Cache-Control: public, max-age=3600` to optimize edge delivery via Cloudflare.

### 2. File Access
`GET /file/*filepath`

Direct asset access via exact path match. 

* **Success (303 See Other)**: If the file exists, the API generates a secure signed URL (1-hour TTL) and redirects the client to the storage provider.
* **Missing (302 Found)**: If an exact match is not found, the API gracefully redirects to `/explorer?q={filepath}` so the user can find the file via fuzzy search.
* **Optimization**: Requests for the same path are deduplicated in-flight.

### 3. Metadata Search
`GET /search?q={query}&limit={n}`

JSON API for fuzzy search on S3 metadata.

* **Parameters**: 
    * `q` (Required): The search term.
    * `limit` (Optional): Integer limit of results (Defaults to 10).
* **Response**: A JSON array of `S3Object` results including metadata and pre-signed `DownloadURL` fields.
* **Optimization**: Uses a composite cache key (`search:query:limit`) to coalesce simultaneous identical searches via `requestGroup`.

### 4. Utility & Monitoring
* `GET /explorer`: The main web interface for browsing and filtering files.
* `GET /health`: System health check and database connectivity status.
* `GET /metrics`: Prometheus formatted performance metrics (scraped by Grafana).

### 5. SEO & Discovery
* `GET /sitemap.xml`: Dynamic XML sitemap for search engine crawlers.
* `GET /robots.txt`: Rules for automated crawlers, optimized to protect `/private/` paths while allowing `/explorer` indexing.

# License
This project is licensed under the MIT License. See the LICENSE file for more information.

# Contact
Email: developer@wikisubmission.org

