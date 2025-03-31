# Cache Server

This is a hyper-optimized in-memory key-value cache service written in Go, designed to handle multiple requests per second.

## Design Choices and Optimizations

### Cache Implementation
- **Ultra-Sharded Architecture**: The cache is divided into 32,768 independent shards to eliminate lock contention entirely.
- **Triple-Tier Hash Function**: Custom hash function optimized for key lengths (fast path for tiny keys, FNV-1a for small-medium keys, xxHash for larger keys).
- **Linked-List Enhanced LRU Eviction**: Combines linked-list scanning with map sampling for optimal eviction speed.
- **Statistical LRU Updates**: Maintains approximated LRU by updating timestamps for only ~0.5% of reads to minimize write contention.
- **Zero-Allocation Operations**: Uses unsafe pointer operations to eliminate any allocation during hot paths.
- **Atomic Counter Arrays**: All statistics tracked using sharded atomic counters to eliminate false sharing.
- **Specialized Memory Layout**: Carefully ordered struct fields to optimize CPU cache behavior and minimize cache line sharing.

### API Implementation
- **Pre-Computed JSON Responses**: All common responses stored as static byte arrays for zero allocation.
- **Hyper-Optimized Query Parsing**: Multi-tier fast paths for key extraction with direct byte access.
- **Enhanced Buffer Pooling**: Thread-local buffer pools with pre-warmed capacity planning.
- **Advanced Object Pooling**: Request objects pre-allocated with warmup phase for zero GC pressure under load.
- **Direct Memory JSON Generation**: Zero-allocation response building using pre-calculated buffer sizes.
- **Header Byte Caching**: Uses byte arrays for header constants to prevent string conversions.
- **Cache-Optimized Handler Code**: Request handlers designed for CPU cache efficiency and branch prediction.

### Server Tuning
- **Extreme GC Settings**: GC percentage set to 1000% to virtually eliminate GC overhead during normal operation.
- **Strategic Memory Pre-allocation**: 400MB pre-allocations with page-level touch patterns to optimize memory layout.
- **Platform-Specific Socket Optimization**: Native socket option tuning for both Unix and Windows systems.
- **TCP_NODELAY and TCP_QUICKACK**: Disabled Nagle's algorithm and enabled immediate ACKs for reduced latency.
- **Advanced Buffer Sizing**: 16MB socket buffers for maximum throughput under bursty loads.
- **Extended Read/Write Timeouts**: 800ms timeouts synchronized with client side for improved reliability.
- **Optimal Thread Management**: Thread pinning and aggressive GOMAXPROCS settings for maximum parallelism.
- **Runtime Optimization Flags**: Custom runtime flags to tune Go's scheduler and GC for extreme performance.
- **Intensive Cache Warmup**: Sophisticated pre-warmup with 300K items to reach optimal performance immediately.

## Building and Running

### Requirements
- Go 1.22 or later

### Using Go directly

```bash
# Build the cache server
go build -o CacheServer .

# Run the server (default port: 7171)
./CacheServer
```

### Using Docker

```bash
# Build the Docker image
docker build -t cacheserver .

# Run the container
docker run -p 7171:7171 cacheserver
```

## API Endpoints

### PUT Operation
- **HTTP Method**: POST
- **Endpoint**: `/put`
- **Request Body**: 
```json
{
  "key": "string (max 256 characters)",
  "value": "string (max 256 characters)"
}
```

### GET Operation
- **HTTP Method**: GET  
- **Endpoint**: `/get`
- **Query Parameter**: `key`
- **Example**: `/get?key=example`

### Additional Endpoints
- **Health Check**: `/health` - Returns server health status
- **Stats**: `/stats` - Returns cache statistics

## Multi-Platform Support

The cache server is designed to work optimally on both Unix-based systems (Linux, macOS, FreeBSD) and Windows, with platform-specific optimizations applied automatically. The integrated approach ensures maximum performance across all deployment platforms.

---

Made with ❤️ by Manan