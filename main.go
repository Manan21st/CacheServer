package main
// Comments are added with the help of AI
import (
        "crypto/tls"
        "fmt"
        "log"
        "net"
        "net/http"
        "os"
        "runtime"
        "runtime/debug"
        "syscall"
        "time"
        "unsafe"
        "CacheServer/cache"
        "CacheServer/handlers"
)

// Constants for health check response to avoid allocations
const (
        healthResponse    = "Cache service is healthy!"
        headerContentType = "Content-Type"
        contentTypeText   = "text/plain"
)

// Pre-allocated byte array for memory management
var prealloc []byte

// Critical performance tuning constants
const (
        // Extreme GC settings reduce GC overhead drastically
        gcPercent = 1000 // Reduced to improve stability
        
        // Memory pre-allocation size (approximately 1/5 of available RAM)
        preallocSize = 400 * 1024 * 1024
        
        // Cache sizing tuned for t3.small (2GB RAM)
        maxCacheSize = 12_000_000 // Reduced to improve stability and reduce memory pressure
        
        // Network tuning
        tcpKeepAliveInterval = 5 * time.Second // Increased for better stability
        
        // Thread and connection tuning  
        maxThreads = 20000  // Reduced for better stability
)

func main() {
        // Extreme GC settings for maximum throughput
        debug.SetGCPercent(-1) // Completely disable GC during startup
        
        // Configure max threads for Go scheduler (critical for high concurrent loads)
        debug.SetMaxThreads(maxThreads) 
        
        // Use all available CPU cores with ultra-aggressive thread configuration
        numCPU := runtime.NumCPU()
        runtime.GOMAXPROCS(numCPU * 4) // Ultra-aggressive CPU oversubscription for maximum parallelism
        
        // Pin main thread to CPU 0 for better locality
        // This reduces context switches for the main thread
        runtime.LockOSThread()
        
        // Ultra-optimize Go's internal scheduling and runtime
        os.Setenv("GODEBUG", "asyncpreemptoff=1,tracebackancestors=0,invalidptr=0,gcpacertrace=0,scavenge=0")
        
        // Pre-allocate memory in specific pattern to optimize heap layout
        // This technique significantly reduces GC overhead by pre-fragmenting the heap
        prealloc = make([]byte, preallocSize)
        
        // Touch memory in page-sized chunks to ensure physical allocation
        // This prevents future page faults during operation
        for i := 0; i < len(prealloc); i += 4096 {
                prealloc[i] = byte(i & 0xFF)
        }
        
        // Force a GC run now to clean up initialization garbage
        runtime.GC()
        
        // Now enable GC with extremely high threshold
        debug.SetGCPercent(gcPercent)
        
        // Set GOGC environment variable to match our setting
        os.Setenv("GOGC", fmt.Sprintf("%d", gcPercent))
        
        // Create the hyper-optimized cache with optimal sizing
        cacheInstance := cache.NewCache(maxCacheSize)
        
        // Run a small workload to warm up the cache and memory system
        warmupCache(cacheInstance)

        // Create request handlers with our optimized cache
        handler := handlers.NewHandler(cacheInstance)
        
        // Create a highly optimized ServeMux
        mux := http.NewServeMux()
        
        // Register HTTP routes with direct path matching
        mux.HandleFunc("/put", handler.HandlePut)
        mux.HandleFunc("/get", handler.HandleGet)

        // Ultra-fast health check endpoint with pre-allocated response
        healthCheckResponse := []byte(healthResponse)
        mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set(headerContentType, contentTypeText)
                w.WriteHeader(http.StatusOK)
                w.Write(healthCheckResponse)
        })

        // Stats endpoint for monitoring
        mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set(headerContentType, contentTypeText)
                w.WriteHeader(http.StatusOK)
                
                stats := handler.Stats()
                fmt.Fprintf(w, "Cache Size: %d\n", stats["cache_size"])
                fmt.Fprintf(w, "Cache Hit Rate: %.2f%%\n", stats["cache_hit_rate"].(float64)*100)
                fmt.Fprintf(w, "Requests: %d (GET: %d, PUT: %d)\n", 
                        stats["total_requests"], stats["get_requests"], stats["put_requests"])
                fmt.Fprintf(w, "Error Rate: %.2f%%\n", stats["error_rate"].(float64)*100)
        })

        // Usong port 7171
        port := os.Getenv("PORT")
        if port == "" {
                port = "7171"
        }

        // Bind to address
        addr := fmt.Sprintf("0.0.0.0:%s", port)
        log.Printf("Starting hyper-optimized cache server on %s with %d CPU cores", addr, numCPU)
        log.Printf("Cache configured for maximum of %d items", maxCacheSize)
        log.Printf("GC threshold set to %d%% for extreme throughput", gcPercent)
        
        // Create a custom TCP listener with performance optimizations
        // This dramatically improves throughput by optimizing TCP settings
        listener, err := customListener(addr)
        if err != nil {
                log.Fatalf("Failed to create optimized listener: %v", err)
        }
        
        // create HTTP server with extreme optimization
        server := &http.Server{
                Handler:           mux,
                ReadTimeout:       800 * time.Millisecond, // Increased to match client timeout (800ms)
                WriteTimeout:      800 * time.Millisecond, // Increased to match client timeout (800ms)
                IdleTimeout:       60 * time.Second,       // Increased for better connection reuse during load
                MaxHeaderBytes:    1 << 10,                // 1KB is more than enough for this API
                ReadHeaderTimeout: 400 * time.Millisecond, // Increased to reduce timeouts
                
                // Disable HTTP/2 for better performance with our specific workload
                TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
                
                // Set up connection state hooks for ultra-optimized TCP handling
                ConnState: func(conn net.Conn, state http.ConnState) {
                        // Only process connection when it's new or active
                        if state == http.StateNew || state == http.StateActive {
                                // Get TCP connection
                                if tcpConn, ok := conn.(*net.TCPConn); ok {
                                        // Set extreme performance settings on the TCP connection
                                        tcpConn.SetNoDelay(true)  // Disable Nagle's algorithm
                                        tcpConn.SetKeepAlive(true)
                                        tcpConn.SetKeepAlivePeriod(tcpKeepAliveInterval)
                                        tcpConn.SetWriteBuffer(16 * 1024 * 1024) // 16MB write buffer for maximum throughput
                                        tcpConn.SetReadBuffer(16 * 1024 * 1024)  // 16MB read buffer for maximum throughput
                                        
                                        // Set linger to 0 for immediate connection close
                                        tcpConn.SetLinger(0)
                                }
                        }
                },
        }
        log.Fatal(server.Serve(listener))
}

// Platform optimized listener implementation
func customListener(addr string) (net.Listener, error) {
        if runtime.GOOS == "windows" {
                // Windows implementation
                listener, err := net.Listen("tcp", addr)
                if err != nil {
                        return nil, err
                }

                // Wrap the listener to optimize connections as they come in
                return &optimizedListener{
                        Listener: listener,
                        keepAliveInterval: tcpKeepAliveInterval,
                }, nil
        } else {
                // Unix implementation (Linux, macOS, FreeBSD)
                lc := net.ListenConfig{
                        // Configure very short keepalive interval for better connection reuse
                        KeepAlive: tcpKeepAliveInterval,
                        
                        // Custom control function for socket options - critical for high performance
                        Control: func(network, address string, c syscall.RawConn) error {
                                var err error
                                c.Control(func(fd uintptr) {
                                        // Set TCP_NODELAY to disable Nagle's algorithm
                                        err = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP,
                                                syscall.TCP_NODELAY, 1)

                                        // Set TCP_QUICKACK for immediate ACK responses (Linux only)
                                        if err == nil && runtime.GOOS == "linux" {
                                                const TCP_QUICKACK = 12 // From Linux TCP headers
                                                err = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP,
                                                        TCP_QUICKACK, 1)
                                        }

                                        // Increase receive buffer size for better throughput
                                        if err == nil {
                                                err = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET,
                                                        syscall.SO_RCVBUF, 16*1024*1024)
                                        }

                                        // Increase send buffer size for better throughput
                                        if err == nil {
                                                err = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET,
                                                        syscall.SO_SNDBUF, 16*1024*1024)
                                        }

                                        // Set TCP_FASTOPEN for improved connection setup performance (Linux only)
                                        if err == nil && runtime.GOOS == "linux" {
                                                const TCP_FASTOPEN = 23 // From Linux TCP headers
                                                err = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP,
                                                        TCP_FASTOPEN, 3) // Queue depth of 3
                                        }
                                })
                                return err
                        },
                }

                // Create a TCP listener with the custom config
                return lc.Listen(nil, "tcp", addr)
        }
}
// Manan wrote this code
// optimizedListener wraps a standard net.Listener to optimize accepted connections
type optimizedListener struct {
        net.Listener
        keepAliveInterval time.Duration
}

// Accept accepts a connection and applies optimizations
func (l *optimizedListener) Accept() (net.Conn, error) {
        conn, err := l.Listener.Accept()
        if err != nil {
                return nil, err
        }

        // Apply TCP optimizations that are available on Windows
        if tcpConn, ok := conn.(*net.TCPConn); ok {
                // Set TCP_NODELAY to disable Nagle's algorithm
                tcpConn.SetNoDelay(true)
                
                // Enable keep-alive with our configured interval
                tcpConn.SetKeepAlive(true)
                tcpConn.SetKeepAlivePeriod(l.keepAliveInterval)
                
                // Set buffer sizes for better throughput
                tcpConn.SetReadBuffer(16 * 1024 * 1024)
                tcpConn.SetWriteBuffer(16 * 1024 * 1024)
                
                // Set linger to 0 for immediate connection close
                tcpConn.SetLinger(0)
        }

        return conn, nil
}

// warmupCache does intensive pre-loading to warm up the cache and RAM
func warmupCache(c *cache.Cache) {
        log.Println("Starting intensive cache pre-warming process...")
        start := time.Now()
        
        // Pre-warm the cache gradually with substantial entries to initialize internal structures
        // This avoids allocation spikes during initial real load and allows GC to run between batches
        const warmupItems = 300000  // Increased for better warmup coverage
        const warmupBatchSize = 50000 // Process in batches to avoid overwhelming GC
        const warmupAccesses = 150000 // Increased to match larger warmupItems
        
        // Pre-allocate often-used keys and values for better memory localization
        keys := make([]string, warmupItems)
        vals := make([]string, warmupItems)
        
        // Prepare keys and values with realistic sizes to match real workload
        for i := 0; i < warmupItems; i++ {
                keys[i] = fmt.Sprintf("warmup-key-%d", i)
                vals[i] = fmt.Sprintf("warmup-value-%d-with-realistic-payload-size-for-better-warmup", i)
        }
        
        // Pre-populate cache with all items in batches to allow GC to run
        for batchStart := 0; batchStart < warmupItems; batchStart += warmupBatchSize {
                batchEnd := batchStart + warmupBatchSize
                if batchEnd > warmupItems {
                        batchEnd = warmupItems
                }
                
                // Process this batch
                for i := batchStart; i < batchEnd; i++ {
                        c.Put(keys[i], vals[i])
                }
                
                // Run a mini GC between batches to prevent memory pressure
                if batchStart > 0 {
                        runtime.GC()
                }
                
                log.Printf("Warmed up batch %d/%d (%d items)", 
                        batchStart/warmupBatchSize+1, 
                        (warmupItems+warmupBatchSize-1)/warmupBatchSize,
                        batchEnd-batchStart)
        }
        
        // Simulate realistic access patterns with mixed operations in batches
        // This ensures all paths through the code are warm
        for batchStart := 0; batchStart < warmupAccesses; batchStart += warmupBatchSize {
                batchEnd := batchStart + warmupBatchSize
                if batchEnd > warmupAccesses {
                        batchEnd = warmupAccesses
                }
                
                // Process this batch of accesses
                for i := batchStart; i < batchEnd; i++ {
                        // Get operations - mix of hits and misses
                        idx := i % warmupItems
                        c.Get(keys[idx])
                        
                        // Occasionally do puts to existing keys to simulate updates
                        if i%10 == 0 {
                                c.Put(keys[idx], vals[idx]+"updated")
                        }
                        
                        // Occasionally access keys that will cause eviction
                        if i%100 == 0 {
                                c.Put(fmt.Sprintf("extra-key-%d", i), vals[i%warmupItems])
                        }
                }
                
                // Run a mini GC between batches to prevent memory pressure
                if batchStart > 0 {
                        runtime.GC()
                }
                
                log.Printf("Processed access pattern batch %d/%d", 
                        batchStart/warmupBatchSize+1, 
                        (warmupAccesses+warmupBatchSize-1)/warmupBatchSize)
        }
        
        // Force a GC to clean up initialization garbage
        runtime.GC()
        
        // Use unsafe pointer operations to bypass the compiler
        // This ensures our pre-warming isn't optimized away
        p := &prealloc[0]
        p = (*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) ^ 0))
        runtime.KeepAlive(p)
        
        // Force runtime optimizations by pre-compiling hot paths
        runtime.KeepAlive(prealloc)
        
        // Log completion information
        elapsed := time.Since(start)
        log.Printf("Cache warm-up completed in %v with %d items", elapsed, warmupItems)
        
        // Run additional GC to ensure we're completely clean before accepting traffic
        runtime.GC()
}