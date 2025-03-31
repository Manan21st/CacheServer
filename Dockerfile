FROM golang:1.22-alpine AS builder

# Disable CGO for a static binary
ENV CGO_ENABLED=0

# Create app directory
WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with minimal optimizations
RUN go build -o CacheServer .

# Use a minimal alpine image for the final container
FROM alpine:3.18

# Add CA certificates for HTTPS
RUN apk --no-cache add ca-certificates

# Set working directory
WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/CacheServer .

# Expose port 7171 (required by assignment)
EXPOSE 7171

# Run the cache server
CMD ["/app/CacheServer"]