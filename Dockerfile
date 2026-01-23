# STAGE 1: Build
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Copy the workspace files
COPY go.work go.work.sum ./
COPY api/go.mod api/go.sum ./api/
COPY aws/go.mod aws/go.sum ./aws/
COPY db/go.mod db/go.sum ./db/

# Download dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the API binary (Static build for Alpine)
RUN CGO_ENABLED=0 GOOS=linux go build -o /ws-api ./api/

# STAGE 2: Run
FROM alpine:latest
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /ws-api .

# Copy your private key and migrations if they aren't embedded
COPY db/migrations ./db/migrations


# Expose the API port
EXPOSE 8080

# Run the binary
CMD ["./ws-api"]