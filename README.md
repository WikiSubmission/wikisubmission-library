# WikiSubmission Library API

Source code repository for the backend services powering [wikisubmission.org](https://wikisubmission.org).

## Description

This project provides a high-performance API designed to index and serve files stored in Amazon S3. It maintains a local metadata index in PostgreSQL to enable fast, fuzzy search capabilities that are natively unavailable in S3.

## System Architecture

The system is designed for high-performance metadata retrieval using the following stack:
* **Backend**: Go (Gin Gonic) with structured JSON logging (`slog`).
* **Database**: PostgreSQL with `pg_trgm` for fuzzy search indexing.
* **Storage**: Amazon S3 with real-time updates via AWS SQS.
* **CDN**: Amazon CloudFront with support for signed URL access to private content.
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
SQS_QUEUE_URL=[https://sqs.us-east-1.amazonaws.com/](https://sqs.us-east-1.amazonaws.com/)...
```
# CloudFront Configuration
```
CLOUDFRONT_BASE_URL=[https://cdn.wikisubmission.org](https://cdn.wikisubmission.org)
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
- `GET /search?q={query}`: Fuzzy search for S3 objects.
- `GET /health`: System health check and DB connectivity status.
- `GET /metrics` : Prometheus formatted performance metrics.

# License
This project is licensed under the MIT License. See the LICENSE file for more information.

# Contact
Email: developer@wikisubmission.org