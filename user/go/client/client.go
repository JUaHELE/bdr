package main

import (
	"bdr/benchmark"
	"bdr/networking"
	"bdr/pause"
	"bdr/utils"
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	xxhash "github.com/zeebo/xxh3"
)

// atomic_t provides atomic operation support for counters
type atomic_t int32

// BufferStats tracks statistics about buffer operations
type BufferStats struct {
	TotalWrites   atomic_t // Counter for total write operations
	OverflowCount atomic_t // Counter for buffer overflow events
	TotalReads    atomic_t // Counter for total read operations
}

// BufferInfo contains metadata about the replication buffer
type BufferInfo struct {
	Offset    uint64 // Current offset position in the buffer
	Length    uint64 // Length of data in the buffer
	Last      uint64 // Last position in the buffer that contains data
	Flags     uint32 // Buffer state flags
	MaxWrites uint64 // Maximum number of writes the buffer can store
}

// TargetInfo contains details about the target device
type TargetInfo struct {
	PageSize       uint64 // Size of a memory page
	WriteInfoSize  uint32 // Size of a write information structure
	BufferByteSize uint64 // Total size of the buffer in bytes
	BitmapByteSize uint64 // Total size of overflow bitmap in bits
}

type State int

const (
	StateHashing State = iota
	StateWriting
	StateReplicating
)

// Client represents a BDR client instance
type Client struct {
	Config            *Config                // Client configuration
	TargetInfo        *TargetInfo            // Information about the target device
	Conn              net.Conn               // Network connection to the server
	Encoder           *gob.Encoder           // For encoding outgoing packets
	Decoder           *gob.Decoder           // For decoding incoming packets
	UnderDevFile      *os.File               // File handle for the underlying device
	CharDevFile       *os.File               // Path to character device used for communication with kernel
	Buf               []byte                 // Shared buffer between kernel and userspace where writes will be saved
	MonitorPauseContr *pause.PauseController // Used to pause monitor changes function
	TermChan          chan struct{}          // Termination channel for graceful shutdown
	Stats             *benchmark.BenchmarkStats

	state      State
	stateMutex sync.Mutex
}

func (s State) String() string {
	return [...]string{
		"Hashing",
		"Writing",
		"Replicating",
	}[s]
}

func (c *Client) GetState() State {
	c.stateMutex.Lock()
	defer c.stateMutex.Unlock()
	return c.state
}

// SetState attempts to transition the client to a new state
func (c *Client) SetState(newState State) {
	c.stateMutex.Lock()
	defer c.stateMutex.Unlock()

	oldState := c.state
	c.state = newState

	c.VerbosePrintln("Client transitioned from", oldState, "to", newState)
}

func (c *Client) InitiateShutdown() {
	select {
	case <-c.TermChan:
		// already closed
	default:
		close(c.TermChan)
	}
}

var (
	// Buffer state flags
	BufferOverflownFlag     = utils.Bit(0) // Flag indicating buffer overflow has occurred
	HashQueueSize           = 8
	KernelLogicalSectorSize = 512 // Bytes
)

var (
	HashPacketQueueSize = 8
)

const (
	PollInterval           = 100 // Polling interval in milliseconds
	RetryInterval          = 1   // Retry interval in seconds
	ReadInterval           = 5
	BenchmarkPrintInterval = 10 // seconds
)

// RetrySleep pauses execution for the configured retry interval
func RetrySleep() {
	time.Sleep(RetryInterval * time.Second)
}

func ReadSleep() {
	time.Sleep(ReadInterval * time.Second)
}

// Println prints a message with the configured verbosity level
func (c *Client) Println(args ...interface{}) {
	c.Config.Println(args...)
}

// VerbosePrintln prints a verbose message if verbose mode is enabled
func (c *Client) VerbosePrintln(args ...interface{}) {
	c.Config.VerbosePrintln(args...)
}

// DebugPrintln prints a debug message if debug mode is enabled
func (c *Client) DebugPrintln(args ...interface{}) {
	c.Config.DebugPrintln(args...)
}

// Print outputs the BufferStats in a human-readable format
func (b BufferStats) Print() {
	fmt.Printf("BufferStats { TotalWrites: %d, OverflowCount: %d, TotalReads: %d }\n", b.TotalWrites, b.OverflowCount, b.TotalReads)
}

// Print outputs the BufferInfo in a human-readable format
func (b BufferInfo) Print() {
	fmt.Printf("BufferInfo { Offset: %d, Length: %d, Last: %d, Flags: %d, MaxWrites: %d }\n", b.Offset, b.Length, b.Last, b.Flags, b.MaxWrites)
}

// Print outputs the TargetInfo in a human-readable format
func (t TargetInfo) Print() {
	fmt.Printf("TargetInfo { PageSize: %d, WriteInfoSize: %d, BufferByteSize: %d }\n", t.PageSize, t.WriteInfoSize, t.BufferByteSize)
}

// CheckOverflow determines if the buffer has overflowed
func (b BufferInfo) CheckOverflow() bool {
	return (b.Flags & BufferOverflownFlag) != 0
}

// HasNewWrites determines if there are new writes in the buffer
func (b BufferInfo) HasNewWrites() bool {
	return b.Length != 0
}

// CheckTermination checks if termination signal has been received
func (c *Client) CheckTermination() bool {
	for {
		select {
		case <-c.TermChan:
			return true
		default:
			return false
		}
	}
}

func (c *Client) SendPacket(packet *networking.Packet) {
	for {
		err := c.Encoder.Encode(packet)
		if err != nil {
			if terminated := c.CheckTermination(); terminated {
				c.VerbosePrintln("Terminating attepmt for packet send...")
				return
			}

			c.VerbosePrintln("SendPacket failed: ", err)
			RetrySleep()
			continue
		}
		break
	}
}

func CreateInfoPacket(packetType uint32) *networking.Packet {
	packet := &networking.Packet{
		PacketType: packetType,
		Payload:    nil,
	}

	return packet
}

// ResetBufferIOCTL resets the buffer using an IOCTL command
func (c *Client) ResetBufferIOCTL() {
	c.VerbosePrintln("Reseting buffer...")
	c.ServeIOCTL(BDR_CMD_RESET_BUFFER, 0)
}

// GetBufferInfoByteOffset calculates the byte offset in the buffer for a given logical offset
func (c *Client) GetBufferInfoByteOffset(bufferInfo *BufferInfo, off uint64) uint64 {
	offset := ((bufferInfo.Offset + off) % bufferInfo.MaxWrites) * uint64(c.TargetInfo.WriteInfoSize)
	return offset
}

// executeIOCTL executes an IOCTL command and returns any error
func (c *Client) executeIOCTL(cmd uintptr, arg uintptr) error {
	err := ioctl(c.CharDevFile.Fd(), cmd, arg)
	if err != nil {
		cmdName := GetIOCTLCommandName(cmd)
		return fmt.Errorf("%s (0x%X) failed: %v", cmdName, cmd, err)
	}
	return nil
}

// serveIOCTL is a wrapper around ioctl calls that retries until successful or termination
func (c *Client) ServeIOCTL(cmd uintptr, arg uintptr) {
	for {
		// Check for termination BEFORE attempting IOCTL
		if terminated := c.CheckTermination(); terminated {
			c.VerbosePrintln("Terminating IOCTL operation due to shutdown...")
			return
		}

		err := c.executeIOCTL(cmd, arg)
		if err != nil {
			if terminated := c.CheckTermination(); terminated {
				c.VerbosePrintln("Terminating attempt for successful ioctls...")
				return
			}

			c.VerbosePrintln("serveIOCTL failed: ", err)
			RetrySleep()
			continue
		}
		break
	}
}

// reconnectToServer attempts to reestablish a connection to the server
func (c *Client) reconnectToServer() {
	c.VerbosePrintln("Trying to reconnect to the server.")
	c.Stats.RecordReconnection()
	// Close existing connection if any
	if c.Conn != nil {
		c.Conn.Close()
	}

	// Build server address string
	address := fmt.Sprintf("%s:%d", c.Config.IpAddress, c.Config.Port)

	for {
		// Check for termination request
		if terminated := c.CheckTermination(); terminated {
			c.VerbosePrintln("Terminating attepmt for reconnection...")
			return
		}

		// Attempt to connect with timeout
		conn, err := net.DialTimeout("tcp", address, 2*time.Second)
		if err == nil {

			// Resend initialization packet
			deviceSize, err := utils.GetDeviceSize(c.Config.UnderDevicePath)
			if err != nil {
				conn.Close()
				RetrySleep()
				continue
			}

			// Create initialization packet with device information
			initPacket := networking.InitInfo{
				DeviceSize:     deviceSize,
				WriteInfoSize:  c.TargetInfo.WriteInfoSize,
				BufferByteSize: c.TargetInfo.BufferByteSize,
			}

			packet := networking.Packet{
				PacketType: networking.PacketTypeInfoInit,
				Payload:    initPacket,
			}

			encoder := gob.NewEncoder(conn)

			// Send the initialization packet
			err = encoder.Encode(packet)
			if err != nil {
				if terminated := c.CheckTermination(); terminated {
					c.VerbosePrintln("Terminating attepmt for packet send...")
					return
				}

				c.VerbosePrintln("Can't reconnect to server: ", err)
				RetrySleep()
				conn.Close()
				continue
			}

			c.Conn = conn
			c.Encoder = encoder
			c.Decoder = gob.NewDecoder(conn)

			// Request hash check if needed
			state := c.GetState()
			if state == StateHashing {
				c.StartHashing()
			} else if state == StateReplicating {
				c.StartHashing()
			} else if state == StateWriting {
				c.StartHashing()
			}

			c.Println("Successfully reconnected to server")
			return
		}

		c.VerbosePrintln("Attempt for reconnection failed. Trying again...")
		RetrySleep()
	}
}

// receivePacket receives a network packet, reconnecting if necessary
func (c *Client) ReceivePacket(packet *networking.Packet) {
	for {
		if terminated := c.CheckTermination(); terminated {
			c.VerbosePrintln("Terminating packet receive due to shutdown...")
			return
		}

		if c.Conn != nil {
			c.Conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		}

		err := c.Decoder.Decode(packet)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// This was a timeout, just retry with termination check
				continue
			}

			if terminated := c.CheckTermination(); terminated {
				c.VerbosePrintln("Terminating attempt for successful packet receive...")
				return
			}

			if err == io.EOF {
				c.reconnectToServer()
				continue
			}

			c.VerbosePrintln("receivePacket failed: ", err)
			RetrySleep()
			continue
		}
		break
	}
}

func (c *Client) SendInitPacket(device string) error {
	// Get total size of the device
	deviceSize, err := utils.GetDeviceSize(device)
	if err != nil {
		return err
	}

	// Create initialization packet with device information
	initPacket := networking.InitInfo{
		DeviceSize:     deviceSize,
		WriteInfoSize:  c.TargetInfo.WriteInfoSize,
		BufferByteSize: c.TargetInfo.BufferByteSize,
	}

	packet := networking.Packet{
		PacketType: networking.PacketTypeInfoInit,
		Payload:    initPacket,
	}

	// Send the initialization packet
	c.SendPacket(&packet)

	return nil
}

// sendCorrectBlock sends the correct data for a block that failed hash verification
func (c *Client) SendCorrectBlock(buf []byte, offset uint64, size uint32) {
	// Create a correct block info packet
	correctBlockInfo := &networking.CorrectBlockInfo{
		Offset: offset,
		Size:   size,
		Data:   buf,
	}

	// Wrap in a network packet
	correctBlockPacket := &networking.Packet{
		PacketType: networking.PacketTypePayloadCorrectBlock,
		Payload:    correctBlockInfo,
	}

	// Send the packet
	c.SendPacket(correctBlockPacket)
}

func (c *Client) StartStatsPrinting(interval time.Duration) {
	if !c.Config.Benchmark {
		return
	}

	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				c.Stats.PrintStats()
			case <-c.TermChan:
				ticker.Stop()
				return
			}
		}
	}()
}

// NewClient creates and initializes a new client instance
func NewClient(cfg *Config) (*Client, error) {
	// Open the character device for communication with kernel
	charDevFd, err := os.OpenFile(cfg.CharDevicePath, os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("falied to open charecter device: %w", err)
	}

	// Get target device information via IOCTL
	var targetInfo TargetInfo
	err = ioctl(charDevFd.Fd(), BDR_CMD_GET_TARGET_INFO, uintptr(unsafe.Pointer(&targetInfo)))
	if err != nil {
		charDevFd.Close()
		return nil, fmt.Errorf("ioctl get info from target failed: %v", err)
	}

	// Memory map the shared buffer
	buf, err := syscall.Mmap(int(charDevFd.Fd()), 0, int(targetInfo.BufferByteSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		charDevFd.Close()
		return nil, fmt.Errorf("mmap failed: %w", err)
	}

	// Connect to remote server
	address := fmt.Sprintf("%s:%d", cfg.IpAddress, cfg.Port)
	conn, err := net.Dial("tcp", address)
	if err != nil {
		charDevFd.Close()
		return nil, fmt.Errorf("failed to connect to receiver: %w", err)
	}

	// Setup gob encoder/decoder for network communication
	encoder := gob.NewEncoder(conn)
	decoder := gob.NewDecoder(conn)

	// Open the underlying device for replication
	underDevFd, err := os.OpenFile(cfg.UnderDevicePath, os.O_RDWR, 0600)
	if err != nil {
		charDevFd.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to open target device: %w", err)
	}

	// Create pause controller for monitor function
	monitorPauseContr := pause.NewPauseController()

	// Create and return the client instance
	return &Client{
		Config:            cfg,
		TargetInfo:        &targetInfo,
		Conn:              conn,
		Encoder:           encoder,
		Decoder:           decoder,
		UnderDevFile:      underDevFd,
		CharDevFile:       charDevFd,
		Buf:               buf,
		MonitorPauseContr: monitorPauseContr,
		TermChan:          make(chan struct{}),
		Stats:             benchmark.NewBenchmarkStats(cfg.Benchmark),
		state:             StateWriting,
	}, nil
}

// CloseClientConn closes the network connection
func (c *Client) CloseClientConn() {
	if c.Conn != nil {
		c.Conn.Close()
	}
}

// CloseResources releases all resources held by the client
func (c *Client) CloseResources() {
	if c.UnderDevFile != nil {
		c.UnderDevFile.Close()
	}
	if c.CharDevFile != nil {
		c.CharDevFile.Close()
	}
	if c.Buf != nil {
		syscall.Munmap(c.Buf)
	}
}

// InitiateCheckedReplication starts a full device verification
func (c *Client) StartHashing() {
	c.VerbosePrintln("Initiating scan with journal...")
	// Reset buffer since we'll perform full verification
	c.ResetBufferIOCTL()

	// Pause the monitor to prevent interference during verification
	c.MonitorPauseContr.Pause()

	c.SetState(StateHashing)
	// Request hashes from server to verify consistency
	packet := networking.Packet{
		PacketType: networking.PacketTypeCmdStartHashing,
		Payload:    nil,
	}

	c.SendPacket(&packet)
}

// ProcessBufferInfo processes write operations stored in the buffer
func (c *Client) ProcessBufferInfo(bufferInfo *BufferInfo, mainLoop bool) {
	// Process each write operation in the buffer
	for i := uint64(0); i < bufferInfo.Length; i++ {
		// Skip if monitor is paused (during hash verification)
		if c.MonitorPauseContr.IsPaused() && mainLoop {
			return
		}

		for {
			// Calculate buffer position
			bufBegin := c.GetBufferInfoByteOffset(bufferInfo, i)
			bufEnd := bufBegin + uint64(c.TargetInfo.WriteInfoSize)
			data := c.Buf[bufBegin:bufEnd]

			reader := bytes.NewReader(data)

			var writeInfoPacket networking.WriteInfo
			var err error

			var sector uint32
			// Read sector number
			if err = binary.Read(reader, binary.LittleEndian, &sector); err != nil {
				if terminated := c.CheckTermination(); terminated {
					c.VerbosePrintln("Terminating attempt to process buffer info...")
					return
				}
				c.VerbosePrintln("Failed to read Sector: ", err)
				RetrySleep()
				continue
			}
			writeInfoPacket.Offset = uint64(KernelLogicalSectorSize) * uint64(sector)

			// Read data size
			if err = binary.Read(reader, binary.LittleEndian, &writeInfoPacket.Size); err != nil {
				if terminated := c.CheckTermination(); terminated {
					c.VerbosePrintln("Terminating attempt to process buffer info...")
					return
				}
				c.VerbosePrintln("Failed to read Size: ", err)
				RetrySleep()
				continue
			}

			// Read the actual data
			writeInfoPacket.Data = make([]byte, c.TargetInfo.PageSize)
			if _, err = io.ReadFull(reader, writeInfoPacket.Data); err != nil {
				if terminated := c.CheckTermination(); terminated {
					c.VerbosePrintln("Terminating attempt to process buffer info...")
					return
				}
				c.VerbosePrintln("Failed to read Data: ", err)
				RetrySleep()
				continue
			}

			// Send the write operation to the server for replication
			packet := networking.Packet{
				PacketType: networking.PacketTypePayloadBufferWrite,
				Payload:    writeInfoPacket,
			}

			c.SendPacket(&packet)

			// Successfully processed this write info, continue to next
			break
		}
	}
}

// MonitorChanges continuously monitors for changes to replicate
func (c *Client) MonitorChanges(wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		// Check for termination or pause
		if terminated := c.MonitorPauseContr.WaitIfPaused(c.TermChan); terminated {
			c.VerbosePrintln("Stopping change monitoring due to shutdown.")
			return
		}

		// Get buffer information via IOCTL
		var bufferInfo BufferInfo
		c.ServeIOCTL(BDR_CMD_READ_BUFFER_INFO, uintptr(unsafe.Pointer(&bufferInfo)))
		// Check if there are new writes to process
		newWrites := bufferInfo.HasNewWrites()
		if !newWrites {
			time.Sleep(PollInterval * time.Millisecond)
			continue
		}

		// Check for buffer overflow
		overflow := bufferInfo.CheckOverflow()
		if overflow {
			c.VerbosePrintln("Buffer overflown...")

			c.Stats.RecordBufferOverflow()
			// Initiate full device verification
			c.StartHashing()
			continue
		}

		// Process the write operations in the buffer
		c.ProcessBufferInfo(&bufferInfo, true)
	}
}

func (c *Client) InitReplication(wg *sync.WaitGroup) {
	wg.Done()
	numWorkers := runtime.NumCPU()
	blockSize := networking.HashedSpaceBase

	type readItem struct {
		offset uint64
		size   uint32
	}
	
	type sendItem struct {
		buffer []byte
		offset uint64
		size   uint32
	}
	
	readChan := make(chan readItem, numWorkers)
	sendChan := make(chan sendItem, numWorkers)
	
	diskSize, err := utils.GetDeviceSize(c.Config.UnderDevicePath)
	if err != nil {
		c.VerbosePrintln("Can't initiate replication:", err)
	}

	// First stage: Generate read tasks
	go func() {
		defer close(readChan)
		for offset := uint64(0); offset < diskSize; offset += uint64(blockSize) {
			if c.CheckTermination() {
				return
			}

			size := uint32(blockSize)
			// Handle the last block which might be smaller
			if offset+uint64(size) > diskSize {
				size = uint32(diskSize - offset)
			}
			readChan <- readItem{
				offset: offset,
				size:   size,
			}
		}
	}()
	
	// Second stage: Read blocks from disk
	var readWg sync.WaitGroup
	readWg.Add(numWorkers)
	go func() {
		defer close(sendChan)
		for i := 0; i < numWorkers; i++ {
			go func() {
				defer readWg.Done()
				for item := range readChan {
					if c.CheckTermination() {
						return
					}

					buf := make([]byte, item.size)
					if _, err := c.UnderDevFile.ReadAt(buf, int64(item.offset)); err != nil && err != io.EOF {
						c.VerbosePrintln("Error while reading the disk:", err)
						continue
					}
					sendChan <- sendItem{
						buffer: buf,
						offset: item.offset,
						size:   item.size,
					}
				}
			}()
		}
		readWg.Wait()
	}()
	
	// Third stage: Send blocks through network
	var sendWg sync.WaitGroup
	sendWg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer sendWg.Done()
			for item := range sendChan {
				if c.CheckTermination() {
					return
				}
				// Send the block data through the network
				c.SendReplicationBlock(item.buffer, item.offset, item.size)
				
				c.DebugPrintln("Block sent successfully, offset:", item.offset, "size:", item.size)
			}
		}()
	}
	
	sendWg.Wait()
	
	packet := CreateInfoPacket(networking.PacketTypeInfoReplicationCompleted)
	c.SendPacket(packet)

}

func (c *Client) SendReplicationBlock(data []byte, offset uint64, size uint32) {
	replicationInfo := networking.ReplicationBlockInfo{
		Offset: offset,
		Size:   size,
		Data:   data,
	}
	
	packet := &networking.Packet{
		PacketType:    networking.PacketTypePayloadReplicationBlock,
		Payload: replicationInfo,
	}
	
	c.SendPacket(packet)
}

func (c *Client) InitHashing(hashChan chan *networking.Packet, hashWg *sync.WaitGroup) {
	defer hashWg.Done()
	numWorkers := runtime.NumCPU()
	type workItem struct {
		hashInfo networking.HashInfo
		buffer   []byte
	}
	type compItem struct {
		hashInfo networking.HashInfo
		buffer   []byte
		hash     uint64
	}
	workChan := make(chan workItem, numWorkers)
	compChan := make(chan compItem, numWorkers)
	hashTimer := benchmark.NewTimer("Hash processing")
	totalBytesHashed := uint64(0)

	// First stage: Read from hashChan and produce work items
	var hashReadWg sync.WaitGroup
	hashReadWg.Add(numWorkers)
	go func() {
		defer close(workChan)
		for i := 0; i < numWorkers; i++ {
			go func() {
				defer hashReadWg.Done()
				for packet := range hashChan {
					hashInfo, ok := packet.Payload.(networking.HashInfo)
					if !ok {
						c.VerbosePrintln("invalid payload for type for HashInfo")
						c.StartHashing()
						continue
					}
					buf := make([]byte, hashInfo.Size)
					if _, err := c.UnderDevFile.ReadAt(buf, int64(hashInfo.Offset)); err != nil && err != io.EOF {
						c.VerbosePrintln("Error while reading the disk...")
						c.StartHashing()
						continue
					}
					workChan <- workItem{
						hashInfo: hashInfo,
						buffer:   buf,
					}
				}
			}()
		}
		hashReadWg.Wait()
	}()

	// Second stage: Process work items and compute hashes
	var workWg sync.WaitGroup
	workWg.Add(numWorkers)
	go func() {
		defer close(compChan)
		for i := 0; i < numWorkers; i++ {
			go func() {
				defer workWg.Done()
				for work := range workChan {
					hash := xxhash.Hash(work.buffer)
					compChan <- compItem{
						hashInfo: work.hashInfo,
						buffer:   work.buffer,
						hash:     hash,
					}
					atomic.AddUint64(&totalBytesHashed, uint64(work.hashInfo.Size))
				}
			}()
		}
		workWg.Wait()
	}()

	// Final stage: Compare hashes and handle mismatches
	var compWg sync.WaitGroup
	compWg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer compWg.Done()
			for comp := range compChan {
				if comp.hash != comp.hashInfo.Hash {
					c.DebugPrintln("Blocks are not equal on offset:", comp.hashInfo.Offset)
					// Send the correct block data if hashes don't match
					c.SendCorrectBlock(comp.buffer, comp.hashInfo.Offset, comp.hashInfo.Size)
				} else {
					c.DebugPrintln("Blocks are equal on offset", comp.hashInfo.Offset)
				}
			}
		}()
	}

	compWg.Wait()

	elapsed := hashTimer.Stop()
	c.Stats.RecordHashing(elapsed, totalBytesHashed)
}

func (c *Client) ProcessBuffer() {
	sent := c.SendBuffer()
	if !sent {
		c.StartHashing()
	} else {
		c.SetState(StateWriting)
		c.MonitorPauseContr.Resume()

		packet := CreateInfoPacket(networking.PacketTypeInfoHashingCompleted)
		c.SendPacket(packet)
	}
}

func (c *Client) SendBuffer() bool {
	c.VerbosePrintln("Sending buffer.")

	var bufferInfo BufferInfo
	c.ServeIOCTL(BDR_CMD_READ_BUFFER_INFO, uintptr(unsafe.Pointer(&bufferInfo)))
	// Check if there are new writes to process
	newWrites := bufferInfo.HasNewWrites()
	if !newWrites {
		return true
	}

	// Check for buffer overflow
	overflow := bufferInfo.CheckOverflow()
	if overflow {
		return false
	}

	// Process the write operations in the buffer
	c.ProcessBufferInfo(&bufferInfo, false)

	return true
}

// ListenPackets listens for incoming packets from the server
func (c *Client) ListenPackets(wg *sync.WaitGroup) {
	defer wg.Done()

	var hashWg sync.WaitGroup
	defer hashWg.Wait()

	hashQueue := make(chan *networking.Packet, HashPacketQueueSize)

	for {
		packet := &networking.Packet{}
		c.ReceivePacket(packet)

		if c.CheckTermination() {
			c.VerbosePrintln("Terminating packet listener.")
			close(hashQueue)
			return
		}

		switch packet.PacketType {
		case networking.PacketTypeInfoStartHashing:
			c.VerbosePrintln("Server started hashing.")

			close(hashQueue)
			hashWg.Wait()

			hashQueue = make(chan *networking.Packet, HashPacketQueueSize)

			hashWg.Add(1)
			go c.InitHashing(hashQueue, &hashWg)
		case networking.PacketTypePayloadHash:
			hashQueue <- packet
		case networking.PacketTypeInfoHashingCompleted:
			c.VerbosePrintln("Server completed hashing.")

			close(hashQueue)
			hashWg.Wait()
			c.ProcessBuffer()

			hashQueue = make(chan *networking.Packet, HashPacketQueueSize)
		case networking.PacketTypeErrJournalOverflow:
			close(hashQueue)
			hashWg.Wait()

			c.Println("Journal has overflown, stopping BDR deamon.")
			c.Println("If you wish to start BDR daemon with direct full scan, start it with -fullscan.")
			c.Println("Use with cautious, because the replica will be corrupted while scanning.")
			c.Println("It is advised to resize the journal or backup the replica.")

			c.InitiateShutdown()
			return
		case networking.PacketTypeErrInit:
			c.Println("ERROR: can't verify init info.")
			c.InitiateShutdown()
		case networking.PacketTypeErrInvalidSizes:
			c.Println("ERROR: source disk and replica do not have the same size.")
			c.InitiateShutdown()
		case networking.PacketTypeErrJournalCreate:
			c.Println("ERROR: can't create journal on server: small size.")
			c.InitiateShutdown()
		default:
			c.VerbosePrintln("Unknown packet received:", packet.PacketType)
		}
	}
}

func (c *Client) FullScan() {
	c.VerbosePrintln("Initiating full scan...")
	// Reset buffer since we'll perform full verification
	c.ResetBufferIOCTL()

	// Pause the monitor to prevent interference during verification
	c.MonitorPauseContr.Pause()

	c.SetState(StateHashing)
	// Request hashes from server to verify consistency
	packet := networking.Packet{
		PacketType: networking.PacketTypeCmdStartFullScan,
		Payload:    nil,
	}

	c.SendPacket(&packet)
}


func (c *Client) FullReplicate() {
	c.VerbosePrintln("Initiating full replication...")
	// Reset buffer since we'll perform full verification
	c.ResetBufferIOCTL()

	// Pause the monitor to prevent interference during verification
	c.MonitorPauseContr.Pause()

	c.SetState(StateReplicating)
	// Request hashes from server to verify consistency
	packet := networking.Packet{
		PacketType: networking.PacketTypeInfoStartReplication,
		Payload:    nil,
	}

	c.SendPacket(&packet)
}

// Run starts the client and handles graceful shutdown
func (c *Client) Run() {
	c.Println("Starting bdr client connected to", c.Config.IpAddress, "and port", c.Config.Port)

	var replicationWg sync.WaitGroup
	defer replicationWg.Wait()

	if c.Config.FullScan {
		c.FullScan()
	} else if c.Config.FullReplicate {
		c.FullReplicate()

		replicationWg.Add(1)
		go c.InitReplication(&replicationWg)
	} else if c.Config.InitialReplication {
		c.StartHashing()
	}

	// Start goroutines with wait group for clean shutdown
	var termWg sync.WaitGroup

	// Start change monitor goroutine
	termWg.Add(1)
	go c.MonitorChanges(&termWg)

	// Start packet listener goroutine
	termWg.Add(1)
	go c.ListenPackets(&termWg)

	c.StartStatsPrinting(BenchmarkPrintInterval * time.Second)

	// Set up signal handling for graceful shutdown
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt signal
	select {
	case <-c.TermChan:
	case <-signalChan:
	}

	c.Println("Interrupt signal received. Shutting down...")
	c.InitiateShutdown()

	// Initiate graceful shutdown
	c.CloseClientConn()
	termWg.Wait()

	// Clean up resources
	c.CloseResources()

	c.Stats.PrintStats()
	c.Println("bdr client daemon terminated successfully.")
}

func main() {
	// Parse configuration
	cfg := NewConfig()

	// Validate command line arguments
	err := ValidateArgs(&cfg.CharDevicePath, &cfg.UnderDevicePath, &cfg.IpAddress, &cfg.Port)
	if err != nil {
		log.Fatalf("Invalid arguments: %v", err)
	}

	// Register gob packets for network communication
	networking.RegisterGobPackets()

	// Create client instance
	client, err := NewClient(cfg)
	if err != nil {
		log.Fatalf("Failed to initiate sender: %v", err)
	}

	// Send initial packet to server
	if err := client.SendInitPacket(cfg.UnderDevicePath); err != nil {
		log.Fatalf("failed to sent init packet: %v", err)
	}

	// Start the client
	client.Run()
}
