package handlers

import (
        "encoding/json"
        "net/http"
        "net/url"
        "runtime"
        "strings"
        "sync"
        "sync/atomic"
        "unsafe"

        "CacheServer/cache"
)

// Max size constants
const (
        // Size limits
        maxKeyLength   = 256
        maxValueLength = 256
        
        // Request limit
        maxBodySize = 1024 
        
        // HTTP methods
        methodGet  = "GET"
        methodPost = "POST"
        
        // Protocol constants
        contentTypeHeader = "Content-Type"
        contentTypeJSON   = "application/json"
)

// Manan's code

// HTTP Status code
const (
        statusOK         = 200
        statusBadRequest = 400
        statusNotAllowed = 405
)

// Pre-computed JSON responses
var (
        // Common error responses
        invalidJSONFormatJSON = []byte(`{"status":"ERROR","message":"Invalid JSON format"}`)
        valueTooLongJSON      = []byte(`{"status":"ERROR","message":"Value must not exceed 256 characters"}`)
        keyTooLongJSON        = []byte(`{"status":"ERROR","message":"Key must be between 1 and 256 characters"}`)
        keyNotFoundJSON       = []byte(`{"status":"ERROR","message":"Key not found."}`)
        methodNotAllowedJSON  = []byte(`{"status":"ERROR","message":"Method not allowed"}`)
        
        // Success responses
        okResponseJSON        = []byte(`{"status":"OK","message":"Key inserted/updated successfully."}`)
        
        // JSON building blocks for constructing responses
        jsonStatusOKBytes     = []byte(`{"status":"OK"`)
        jsonKeyPrefixBytes    = []byte(`,"key":"`)
        jsonValuePrefixBytes  = []byte(`","value":"`)
        jsonClosingBytes      = []byte(`"}`)
        
        // Static header key/value pairs
        contentTypeHeaderKey   = []byte("Content-Type")
        contentTypeJSONValue   = []byte("application/json")
)

// Pool sizes
const (
        // Size of response buffer pool (per GOMAXPROCS)
        responseBufferPoolSize = 16384
        
        // Size of request object pool (per GOMAXPROCS)
        requestPoolSize = 16384
        
        // Initial size of response buffers
        initialResponseBufferSize = 1024
)

// Request/response object pooling
var (
        // Ultra-sized pool for request objects 
        putRequestPool = newEnhancedPool(func() interface{} {
                return &PutRequest{}
        }, requestPoolSize * runtime.GOMAXPROCS(0))
        
        // Ultra-sized pool for response buffers with pre-warming
        responseBufferPool = newEnhancedPool(func() interface{} {
                // Pre-allocate a buffer of reasonable size
                buf := make([]byte, 0, initialResponseBufferSize)
                return &buf
        }, responseBufferPoolSize * runtime.GOMAXPROCS(0))
)

// PutRequest represents a PUT operation request
type PutRequest struct {
        Key   string `json:"key"`
        Value string `json:"value"`
}

// Handler implements optimized HTTP handlers
type Handler struct {
        cache           *cache.Cache // cache implementation
        getRequestCount uint64      
        putRequestCount uint64      
        errorCount      uint64       
}

// NewHandler creates a new handler with the given cache
func NewHandler(cache *cache.Cache) *Handler {
        h := &Handler{
                cache: cache,
        }
        
        // Warm up the pools by putting some objects back
        warmPools()
        
        return h
}

// Enhanced object pool with pre-warming
func newEnhancedPool(newFn func() interface{}, capacity int) *sync.Pool {
        pool := &sync.Pool{New: newFn}
        
        // Pre-warm the pool with objects to avoid allocation during peak loads
        objects := make([]interface{}, 0, capacity/2)
        
        // Get objects from the pool to force creation
        for i := 0; i < capacity/2; i++ {
                objects = append(objects, pool.Get())
        }
        
        // Put all the objects back
        for _, obj := range objects {
                pool.Put(obj)
        }
        
        return pool
}

// Warm up the pools to avoid allocation during peak loads
func warmPools() {
        // Pre-warm the PUT request pool with more objects
        var reqs []*PutRequest
        for i := 0; i < 5000; i++ {
                req := putRequestPool.Get().(*PutRequest)
                req.Key = "warmup-key"
                req.Value = "warmup-value"
                reqs = append(reqs, req)
        }
        for _, req := range reqs {
                req.Key = ""
                req.Value = ""
                putRequestPool.Put(req)
        }
        
        // Pre-warm the response buffer pool with more objects
        var bufs []*[]byte
        for i := 0; i < 5000; i++ {
                buf := responseBufferPool.Get().(*[]byte)
                
                // Create a realistic response size to properly pre-allocate
                *buf = append((*buf)[:0], []byte(`{"status":"OK","key":"warmup-key-1234567890","value":"warmup-value-1234567890"}`)...)
                bufs = append(bufs, buf)
        }
        for _, buf := range bufs {
                *buf = (*buf)[:0]
                responseBufferPool.Put(buf)
        }
        
        // Force a GC run now to clean up initialization garbage
        runtime.GC()
}

// directBytes accesses the string's underlying bytes without allocation
// Zero-allocation way to get bytes from a string
func directBytes(s string) []byte {
        // Using unsafe to avoid allocations - this is a high-performance critical path
        return *(*[]byte)(unsafe.Pointer(
                &struct {
                        string
                        cap int
                }{s, len(s)},
        ))
}

// HandlePut handles PUT operations with extreme optimization
func (h *Handler) HandlePut(w http.ResponseWriter, r *http.Request) {
        // Count request
        atomic.AddUint64(&h.putRequestCount, 1)
        
        // Ultra-fast method check - direct string comparison
        if r.Method != methodPost {
                // Direct write header allows us to skip additional allocations
                w.Header().Set(contentTypeHeader, contentTypeJSON)
                w.WriteHeader(statusNotAllowed)
                w.Write(methodNotAllowedJSON)
                atomic.AddUint64(&h.errorCount, 1)
                return
        }
        
        // Apply size limit 
        r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
        
        // Get request object from pool
        req := putRequestPool.Get().(*PutRequest)
        
        // Reset to zero state in case of reuse
        req.Key = ""
        req.Value = ""
        
        // Parse JSON
        decoder := json.NewDecoder(r.Body)
        decoder.DisallowUnknownFields() // prevent unknown fields
        
        if err := decoder.Decode(req); err != nil {
                putRequestPool.Put(req) // Return to pool
                
                // Send error response
                w.Header().Set(contentTypeHeader, contentTypeJSON)
                w.WriteHeader(statusBadRequest)
                w.Write(invalidJSONFormatJSON)
                atomic.AddUint64(&h.errorCount, 1)
                return
        }
        
        // Validate key/value
        keyLen, valueLen := len(req.Key), len(req.Value)
        var invalidKey, invalidValue bool
        
        invalidKey = (keyLen == 0 || keyLen > maxKeyLength)
        invalidValue = (valueLen > maxValueLength)
        
        if invalidKey {
                putRequestPool.Put(req) // Return to pool
                
                // Send error for key
                w.Header().Set(contentTypeHeader, contentTypeJSON)
                w.WriteHeader(statusBadRequest)
                w.Write(keyTooLongJSON)
                atomic.AddUint64(&h.errorCount, 1)
                return
        }
        
        if invalidValue {
                putRequestPool.Put(req) // Return to pool
                
                // Send error for value
                w.Header().Set(contentTypeHeader, contentTypeJSON)
                w.WriteHeader(statusBadRequest)
                w.Write(valueTooLongJSON)
                atomic.AddUint64(&h.errorCount, 1)
                return
        }
        
        // Fast path - store in cache
        h.cache.Put(req.Key, req.Value)
        
        // Return request to pool
        putRequestPool.Put(req)
        
        // Send success response
        w.Header().Set(contentTypeHeader, contentTypeJSON)
        w.WriteHeader(statusOK)
        w.Write(okResponseJSON)
}

// HandleGet handles GET operations with extreme optimization
func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
        // Count request 
        atomic.AddUint64(&h.getRequestCount, 1)
        
        // Ultra-fast method check with direct string comparison
        if r.Method != methodGet {
                w.Header().Set(contentTypeHeader, contentTypeJSON)
                w.WriteHeader(statusNotAllowed)
                w.Write(methodNotAllowedJSON)
                atomic.AddUint64(&h.errorCount, 1)
                return
        }
        
        // extrct key with zero allocations
        var key string
        rawQuery := r.URL.RawQuery
        
        // Hyper-optimizd key extraction with multiple fast paths
        // Each path is optimized for a specific common case
        if len(rawQuery) > 4 && rawQuery[0] == 'k' && rawQuery[1] == 'e' && 
           rawQuery[2] == 'y' && rawQuery[3] == '=' {
                // Fast path 1: key=value with no escaping or other params (most common)
                if !strings.ContainsAny(rawQuery, "%+&") {
                        // Direct byte slice without allocation
                        key = rawQuery[4:]
                } else if i := strings.IndexByte(rawQuery, '&'); i > 4 {
                        // Fast path 2: key=value&other=params
                        // Extract just the key part
                        keyPart := rawQuery[4:i]
                        if !strings.ContainsAny(keyPart, "%+") {
                                // No URL escaping in key
                                key = keyPart
                        } else {
                                // Need to unescape
                                key, _ = url.QueryUnescape(keyPart)
                        }
                } else if !strings.ContainsAny(rawQuery[4:], "%+") {
                        // Fast path 3: key=value with no escaping
                        key = rawQuery[4:]
                } else {
                        // Slow path
                        key, _ = url.QueryUnescape(rawQuery[4:])
                }
        } else {
                // Fallback for non-standard queries
                key = r.URL.Query().Get("key")
        }
        
        // Validate key
        if keyLen := len(key); keyLen == 0 || keyLen > maxKeyLength {
                w.Header().Set(contentTypeHeader, contentTypeJSON)
                w.WriteHeader(statusBadRequest)
                w.Write(keyTooLongJSON)
                atomic.AddUint64(&h.errorCount, 1)
                return
        }
        
        // Get value from cache
        value, found := h.cache.Get(key)
        
        // Set response header
        w.Header().Set(contentTypeHeader, contentTypeJSON)
        w.WriteHeader(statusOK)
        
        if !found {
                // Key not found
                w.Write(keyNotFoundJSON)
                return
        }
        
        // Get buffer from pool
        bufPtr := responseBufferPool.Get().(*[]byte)
        buf := (*bufPtr)[:0] // Reset without reallocation
        
        // Build response with targeted capacity planning
        // Reserve exact space needed to avoid reallocations
        bufCap := len(jsonStatusOKBytes) + len(jsonKeyPrefixBytes) + 
                 len(key) + len(jsonValuePrefixBytes) + 
                 len(value) + len(jsonClosingBytes)
        
        // Ensure buffer has enough capacity
        if cap(*bufPtr) < bufCap {
                // Rare path - need to reallocate
                newBuf := make([]byte, 0, bufCap+64) // Add margin
                *bufPtr = newBuf
                buf = newBuf
        }
        
        // Build response with zero-copy concatenation
        buf = append(buf, jsonStatusOKBytes...)
        buf = append(buf, jsonKeyPrefixBytes...)
        buf = append(buf, directBytes(key)...) // Direct bytes access
        buf = append(buf, jsonValuePrefixBytes...)
        buf = append(buf, directBytes(value)...) // Direct bytes access
        buf = append(buf, jsonClosingBytes...)
        
        // Write response in one call
        w.Write(buf)
        
        // Return buffer to pool
        *bufPtr = buf
        responseBufferPool.Put(bufPtr)
}

// Stats returns cache statistics
func (h *Handler) Stats() map[string]interface{} {
        // Load atomic counters
        gets := atomic.LoadUint64(&h.getRequestCount)
        puts := atomic.LoadUint64(&h.putRequestCount)
        errors := atomic.LoadUint64(&h.errorCount)
        
        // Get cache stats
        cacheStats := h.cache.Stats()
        
        // Calculate total and error rate
        total := gets + puts
        errorRate := float64(0)
        if total > 0 {
                errorRate = float64(errors) / float64(total)
        }
        
        // Build response with clear naming
        return map[string]interface{}{
                "get_requests":   gets,
                "put_requests":   puts,
                "total_requests": total,
                "errors":         errors,
                "error_rate":     errorRate,
                "cache_size":     cacheStats["size"],
                "cache_hit_rate": cacheStats["hit_rate"],
                "cache_hits":     cacheStats["hits"],
                "cache_misses":   cacheStats["misses"],
        }
}