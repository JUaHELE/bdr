package benchmark

import (
	"fmt"
	"sync"
	"time"
)

// BenchmarkStats tracks performance metrics for different operations
type BenchmarkStats struct {
	Up bool
	StartTime          time.Time
	TotalBytesWritten  uint64
	TotalPacketsSent   uint64
	HashingTime        time.Duration
	HashingBytes       uint64
	BufferOverflows    uint64
	ReconnectionEvents uint64
	mu                 sync.Mutex
}

// NewBenchmarkStats creates a new BenchmarkStats instance
func NewBenchmarkStats(up bool) *BenchmarkStats {
	return &BenchmarkStats{
		StartTime: time.Now(),
		Up: up,
	}
}

// RecordWrite adds a write operation to the stats
func (bs *BenchmarkStats) RecordWrite(bytes uint64) {
	if bs.Up == false {
		return
	}

	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.TotalBytesWritten += bytes
	bs.TotalPacketsSent++
}

// RecordHashing records time spent and data processed during hashing
func (bs *BenchmarkStats) RecordHashing(duration time.Duration, bytes uint64) {
	if bs.Up == false {
		return
	}

	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.HashingTime += duration
	bs.HashingBytes += bytes
}

// RecordBufferOverflow increments the buffer overflow counter
func (bs *BenchmarkStats) RecordBufferOverflow() {
	if bs.Up == false {
		return
	}

	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.BufferOverflows++
}

// RecordReconnection increments the reconnection counter
func (bs *BenchmarkStats) RecordReconnection() {
	if bs.Up == false {
		return
	}

	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.ReconnectionEvents++
}

// PrintStats outputs the current performance statistics
func (bs *BenchmarkStats) PrintStats() {
	if bs.Up == false {
		return
	}

	bs.mu.Lock()
	defer bs.mu.Unlock()
	
	elapsed := time.Since(bs.StartTime).Seconds()
	
	fmt.Println("\n--- BDR Performance Statistics ---")
	fmt.Printf("Running time: %.2f seconds\n", elapsed)
	
	// Replication throughput
	if bs.TotalBytesWritten > 0 {
		bytesPerSec := float64(bs.TotalBytesWritten) / elapsed
		fmt.Printf("Replication throughput: %.2f MB/s (%.2f MiB/s)\n", 
			bytesPerSec / 1000000, bytesPerSec / 1048576)
		fmt.Printf("Total data transferred: %.2f GB (%.2f GiB)\n", 
			float64(bs.TotalBytesWritten) / 1000000000, float64(bs.TotalBytesWritten) / 1073741824)
		fmt.Printf("Packets sent: %d (avg %.2f KB/packet)\n", 
			bs.TotalPacketsSent, float64(bs.TotalBytesWritten) / float64(bs.TotalPacketsSent) / 1000)
	}
	
	// Hashing performance
	if bs.HashingBytes > 0 {
		hashingSeconds := bs.HashingTime.Seconds()
		if hashingSeconds > 0 {
			hashRate := float64(bs.HashingBytes) / hashingSeconds
			fmt.Printf("Hashing performance: %.2f MB/s (%.2f MiB/s)\n", 
				hashRate / 1000000, hashRate / 1048576)
			fmt.Printf("Total time spent hashing: %.2f seconds\n", hashingSeconds)
			fmt.Printf("Total data hashed: %.2f GB (%.2f GiB)\n", 
				float64(bs.HashingBytes) / 1000000000, float64(bs.HashingBytes) / 1073741824)
		}
	}
	
	// Error events
	fmt.Printf("Buffer overflows: %d\n", bs.BufferOverflows)
	fmt.Printf("Reconnection events: %d\n", bs.ReconnectionEvents)
}

// Timer provides a simple way to time code blocks
type Timer struct {
	start time.Time
	name  string
}

// NewTimer creates a new timer
func NewTimer(name string) *Timer {
	return &Timer{
		start: time.Now(),
		name:  name,
	}
}

// Stop ends the timer and returns the elapsed duration
func (t *Timer) Stop() time.Duration {
	return time.Since(t.start)
}

// StopAndPrint ends the timer and prints the elapsed time
func (t *Timer) StopAndPrint() time.Duration {
	elapsed := time.Since(t.start)
	fmt.Printf("[BENCHMARK] %s took %v\n", t.name, elapsed)
	return elapsed
}

