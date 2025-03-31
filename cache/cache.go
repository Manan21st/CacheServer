package cache

import (
        "math"
        "runtime"
        "sync"
        "sync/atomic"
        "unsafe"
)

// Ultra high shard count to eliminate lock contention entirely
// Using a much higher number of shards than CPUs ensures minimal contention
const ShardCount = 32768

// Constants for both hash functions 
const (
        // FNV-1a constants for small keys
        fnvPrime        = 16777619
        fnvOffset       = 2166136261
        
        // xxHash constants for larger keys
        prime1 = 11400714785074694791
        prime2 = 14029467366897019727
        prime3 = 1609587929392839161
        prime4 = 9650029242287828579
        prime5 = 2870177450012600261
        
        // Special constants
        magicMix         = 0x9E3779B9 // Magic constant from Fibonacci hashing
        shardBits        = 15         // log2 of ShardCount (32768 = 2^15)
        shardMask        = ShardCount - 1
        samplingInterval = 200        // Increased to further reduce lock contention (higher = less contention)
        evictionSamples  = 3          // Further reduced to improve speed during high load (lower = faster)
)

// Flags for entry states
type entryFlag uint8

const (
        entryNormal    entryFlag = iota // Normal, active entry
        entryEvicting                    // Entry is being evicted (optimization flag)
)

// entry represents a key-value pair in the cache
// optimized memory layout: all related fields close together for CPU cache efficiency
type entry struct {
        key      string   // The key string
        value    string   // The value string 
        lastUsed uint32   // Timestamp for LRU (32-bit for memory efficiency)
        flags    entryFlag // Status flags
        next     *entry   // For eviction optimization (avoid map iteration)
}

// shard represents a portion of the cache with its own lock
// Cache-line optimized: keeps hot fields close together
type shard struct {
        mu        sync.RWMutex    // Lock for this shard
        items     map[string]*entry // Items in this shard
        itemCount uint32          // Number of items in this shard (atomic)
        capacity  uint32          // Maximum capacity for this shard
        clock     uint32          // Local logical clock for this shard
        evictions uint32          // Eviction counter for this shard
        head      *entry          // Most recently used item pointer
        hits      uint32          // Hit counter for this shard
        misses    uint32          // Miss counter for this shard
        
        // Cache padding to avoid false sharing
        _padding [56]byte 
}

// Cache is the main cache structure
type Cache struct {
        shards      [ShardCount]shard // Fixed size array of shards
        maxItems    uint32            // Maximum total items allowed
}

// directKey accesses the string's underlying bytes without allocation
// This is a zero-allocation way to get the bytes of a string
func directKey(s string) []byte {
        // This is safe because we're only reading the bytes
        // and string is immutable in Go
        return *(*[]byte)(unsafe.Pointer(
                &struct {
                        string
                        cap int
                }{s, len(s)},
        ))
}

// ultraFastHash is a hyper-optimized hash function
// Optimized for both tiny keys and longer keys
func ultraFastHash(key string) uint64 {
        // Optimize for very small keys (1-4 bytes) - extremely common in caches
        keyLen := len(key)
        
        if keyLen <= 4 {
                // Use an extremely fast method for tiny keys
                // This is faster than any algorithm for very short strings
                var h uint64
                for i := 0; i < keyLen; i++ {
                        h = (h << 5) + h + uint64(key[i])
                }
                return h
        }
        
        // For small-medium keys (5-16 bytes), use FNV-1a
        if keyLen <= 16 {
                var h uint64 = fnvOffset
                
                // Use direct key access without allocation
                b := directKey(key)
                
                // Unrolled for small keys
                for i := 0; i < keyLen; i++ {
                        h ^= uint64(b[i])
                        h *= fnvPrime
                }
                return h
        }
        
        // For larger keys, use a xxHash inspired algorithm
        // Get direct access to the bytes without allocation
        b := directKey(key)
        
        var h uint64 = prime5
        i := 0
        
        // Process 8 bytes chunks with minimal branching
        for l := keyLen - 8; i <= l; i += 8 {
                // Process chunks with direct memory access
                k := *(*uint64)(unsafe.Pointer(&b[i]))
                k *= prime2
                k = (k << 31) | (k >> 33) // rotate left by 31 bits
                k *= prime1
                h ^= k
                h = (h << 27) | (h >> 37) // rotate left by 27 bits
                h = h*5 + prime4
        }
        
        // Process remaining bytes
        if i < keyLen {
                var remaining uint64
                for shift := uint(0); i < keyLen; i++ {
                        remaining |= uint64(b[i]) << shift
                        shift += 8
                }
                h ^= remaining * prime1
                h = (h << 23) | (h >> 41) // rotate left by 23 bits
                h = h*3 + prime2
        }
        
        // Final mixing
        h ^= h >> 33
        h *= prime2
        h ^= h >> 29
        h *= prime3
        h ^= h >> 32
        
        return h
}

// getShard returns the shard for a given key
// Optimized for zero allocations and maximum speed
func (c *Cache) getShard(key string) *shard {
        hash := ultraFastHash(key)
        return &c.shards[hash&shardMask]
}

// NewCache creates a new cache with the given max size
func NewCache(maxSize int) *Cache {
        // Pre-allocate all structures to avoid allocation later
        c := &Cache{
                maxItems: uint32(maxSize),
        }
        
        // Calculate optimal capacity per shard
        capacity := uint32(maxSize/ShardCount + 5) // +5 for minor overallocation
        
        // Initialize all shards
        for i := 0; i < ShardCount; i++ {
                c.shards[i] = shard{
                        items:    make(map[string]*entry, capacity),
                        capacity: capacity,
                }
        }
        
        // Force memiry allocation now to avoid GC later
        runtime.GC()
        
        return c
}

// Put adds or updates a key-value pair with maximum optimization
func (c *Cache) Put(key, value string) {
        // Get the appropriate shard using the ultra-fast hash
        s := c.getShard(key)
        
        // Update the shard's clock - local clock reduces contention
        // 32-bit is enough for our purposes and half the size
        timestamp := atomic.AddUint32(&s.clock, 1)
        
        // Fast path: try a read lock first to check if key exists
        // This optimization helps in high-update scenarios by
        // avoiding write lock contention when possible
        s.mu.RLock()
        _, found := s.items[key]
        s.mu.RUnlock()
        
        if found {
                // Key exists, take write lock and update
                s.mu.Lock()
                
                // Double-check the key still exists after acquiring write lock
                if e, ok := s.items[key]; ok {
                        e.value = value
                        e.lastUsed = timestamp
                        e.flags = entryNormal
                        
                        e.next = s.head
                        s.head = e
                        
                        s.mu.Unlock()
                        return
                }
                
                // Key was removed between read and write lock, fall through to insert
                found = false
                s.mu.Unlock()
        }
        
        // Key doesn't exist, insert new entry
        s.mu.Lock()
        
        // Check for capacity one more time under write lock
        if atomic.LoadUint32(&s.itemCount) >= s.capacity {
                c.evictFromShard(s)
        }
        
        // Create new entry and insert
        newEntry := &entry{
                key:      key,
                value:    value,
                lastUsed: timestamp,
                flags:    entryNormal,
                next:     s.head,
        }
        s.items[key] = newEntry
        s.head = newEntry
        
        
        atomic.AddUint32(&s.itemCount, 1)
        
        s.mu.Unlock()
}
// Manan's code
// Get retrieves a value for a given key with maximum optimization
func (c *Cache) Get(key string) (string, bool) {
        // Find the right shard
        s := c.getShard(key)
        
        // Try with read lock first
        s.mu.RLock()
        e, found := s.items[key]
        
        if !found {
                // Not found, unlock and return
                s.mu.RUnlock()
                atomic.AddUint32(&s.misses, 1)
                return "", false
        }
        
        // Hit - save value for return
        value := e.value
        
        // Use modulo of key hash to determine if we update LRU
        // This significantly reduces contention by only updating 
        // access time for a small percentage of accesses
        updateLRU := (ultraFastHash(key) % samplingInterval) == 0
        s.mu.RUnlock()
        
        // Register cache hit
        atomic.AddUint32(&s.hits, 1)
        
        // Fast return for non-LRU-update case
        if !updateLRU {
                return value, true
        }
        
        // Only update LRU periodically to reduce lock contention
        s.mu.Lock()
        
        // Double check entry still exists
        if e, stillExists := s.items[key]; stillExists {
                // Update timestamp & move to head of linked list
                e.lastUsed = atomic.AddUint32(&s.clock, 1)
                
                if e != s.head {
                        e.next = s.head
                        s.head = e
                }
        }
        
        s.mu.Unlock()
        return value, true
}

// evictFromShard removes a least recently used item from the shard
// Caller must hold the shard's write lock
func (c *Cache) evictFromShard(s *shard) {
        // Quick check
        if atomic.LoadUint32(&s.itemCount) < s.capacity {
                return
        }
        
        // Prepare for eviction
        var (
                oldestKey       string
                oldestEntry     *entry
                oldestTimestamp uint32 = math.MaxUint32
        )
        
        // Use partail sampling for eviction
        // to find an approximately least recently used entry
        count := 0
        entry := s.head
        
        // first, try linked list sampling 
        for entry != nil && count < evictionSamples {
                if entry.lastUsed < oldestTimestamp && entry.flags != entryEvicting {
                        oldestTimestamp = entry.lastUsed
                        oldestEntry = entry
                        oldestKey = entry.key
                }
                entry = entry.next
                count++
        }
        
        // As a fallback, if we didnt find a good candidste, check the map
        // This provides additional safety for pathological cases
        if oldestKey == "" {
                for k, e := range s.items {
                        if e.lastUsed < oldestTimestamp && e.flags != entryEvicting {
                                oldestTimestamp = e.lastUsed
                                oldestKey = k
                                oldestEntry = e
                        }
                        
                        count++
                        if count >= evictionSamples*2 {
                                break
                        }
                }
        }
        
        // If we found an entry to evict, remove it
        if oldestKey != "" {
                if oldestEntry != nil {
                        oldestEntry.flags = entryEvicting 
                }
                delete(s.items, oldestKey)
                atomic.AddUint32(&s.itemCount, ^uint32(0))
                atomic.AddUint32(&s.evictions, 1)
        }
}

// Stats returns cache statistics
func (c *Cache) Stats() map[string]interface{} {
        var totalSize, hits, misses, evictions uint64
        
        // aggregate stats from all shards
        for i := 0; i < ShardCount; i++ {
                s := &c.shards[i]
                totalSize += uint64(atomic.LoadUint32(&s.itemCount))
                hits += uint64(atomic.LoadUint32(&s.hits))
                misses += uint64(atomic.LoadUint32(&s.misses))
                evictions += uint64(atomic.LoadUint32(&s.evictions))
        }
        
        total := hits + misses
        hitRate := float64(0)
        if total > 0 {
                hitRate = float64(hits) / float64(total)
        }
        
        return map[string]interface{}{
                "size":       totalSize,
                "hits":       hits, 
                "misses":     misses,
                "hit_rate":   hitRate,
                "evictions":  evictions,
        }
}

// Size returns the current number of entries 
func (c *Cache) Size() int {
        var totalSize uint64
        
        // Sum item counts from all shards
        for i := 0; i < ShardCount; i++ {
                totalSize += uint64(atomic.LoadUint32(&c.shards[i].itemCount))
        }
        
        return int(totalSize)
}