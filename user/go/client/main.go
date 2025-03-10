package main

import (
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
	"sync"
	"syscall"
	"time"
	"unsafe"

	simdsha256 "github.com/minio/sha256-simd"
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
}

// Client represents a BDR client instance
type Client struct {
	Config            *Config                 // Client configuration
	TargetInfo        *TargetInfo             // Information about the target device
	Conn              net.Conn                // Network connection to the server
	Encoder           *gob.Encoder            // For encoding outgoing packets
	Decoder           *gob.Decoder            // For decoding incoming packets
	UnderDevFile      *os.File                // File handle for the underlying device 
	CharDevFile       *os.File                // Path to character device used for communication with kernel
	Buf               []byte                  // Shared buffer between kernel and userspace where writes will be saved
	MonitorPauseContr *pause.PauseController  // Used to pause monitor changes function
	TermChan          chan struct{}           // Termination channel for graceful shutdown
}

var (
	// Buffer state flags
	BufferOverflownFlag = utils.Bit(0) // Flag indicating buffer overflow has occurred
)

const (
	PollInterval  = 100 // Polling interval in milliseconds
	RetryInterval = 1   // Retry interval in seconds
)

// RetrySleep pauses execution for the configured retry interval
func RetrySleep() {
	time.Sleep(RetryInterval * time.Second)
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

// ResetBufferIOCTL resets the buffer using an IOCTL command
func (c *Client) ResetBufferIOCTL() {
	c.VerbosePrintln("Reseting buffer...")
	c.serveIOCTL(BDR_CMD_RESET_BUFFER, 0)
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
func (c *Client) serveIOCTL(cmd uintptr, arg uintptr) {
	for {
		err := c.executeIOCTL(cmd, arg)
		if err != nil {
			if terminated := c.CheckTermination(); terminated {
				c.VerbosePrintln("Terminating attepmt for successfull ioctls...")
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
func (c *Client) reconnectToServer(sendHashReq bool) {
	c.VerbosePrintln("Trying to reconnect to the server.")

	// Close existing connection if any
	if c.Conn != nil {
		c.Conn.Close()
	}

	// Build server address string
	address := fmt.Sprintf("%s:%d", c.Config.IpAddress, c.Config.Port)

	for {
		// Check for termination request
		if terminated := c.CheckTermination(); terminated {
			c.VerbosePrintln("Terminating attepmt for reconnect...")
			return
		}

		// Attempt to connect with timeout
		conn, err := net.DialTimeout("tcp", address, 2*time.Second)
		if err == nil {
			// Connection successful
			c.Conn = conn
			c.Encoder = gob.NewEncoder(conn)
			c.Decoder = gob.NewDecoder(conn)

			// Resend initialization packet
			if err := c.SendInitPacket(c.Config.UnderDevicePath); err != nil {
				c.Println("Failed to resend init packet during reconnection")
				continue
			}

			// Request hash check if needed
			if sendHashReq {
				c.InitiateCheckedReplication()
			}

			c.Println("Successfully reconnected to server")
			return
		}

		c.VerbosePrintln("Attempt for reconnection failed. Trying again...")
	}
}

// sendPacket sends a network packet, retrying if necessary
func (c *Client) sendPacket(packet *networking.Packet) {
	for {
		err := c.Encoder.Encode(packet)
		if err != nil {
			if terminated := c.CheckTermination(); terminated {
				c.VerbosePrintln("Terminating attepmt for successfull packet send...")
				return
			}

			c.VerbosePrintln("SendPacket failed: ", err)
			RetrySleep()
			continue
		}
		break
	}
}

// receivePacket receives a network packet, reconnecting if necessary
func (c *Client) receivePacket(packet *networking.Packet, sendHashReq bool) {
	for {
		err := c.Decoder.Decode(packet)
		if err != nil {
			if terminated := c.CheckTermination(); terminated {
				c.VerbosePrintln("Terminating attepmt for successfull packet send...")
				return
			}

			if err == io.EOF {
				// Connection lost, try to reconnect
				c.reconnectToServer(sendHashReq)
				continue
			}

			c.VerbosePrintln("receivePacket failed: ", err)
			RetrySleep()
			continue
		}
		break
	}
}

// SendInitPacket sends an initialization packet with device information
func (c *Client) SendInitPacket(device string) error {
	// Get sector size of the device
	sectorSize, err := utils.GetSectorSize(device)
	if err != nil {
		return err
	}

	// Get total size of the device
	deviceSize, err := utils.GetDeviceSize(device)
	if err != nil {
		return err
	}

	// Create initialization packet with device information
	initPacket := networking.InitInfo{
		SectorSize: sectorSize,
		DeviceSize: deviceSize,
	}

	packet := networking.Packet{
		PacketType: networking.PacketTypeInit,
		Payload: initPacket,
	}

	// Send the initialization packet
	c.sendPacket(&packet)

	return nil
}

// sendCorrectBlock sends the correct data for a block that failed hash verification
func (c *Client) sendCorrectBlock(buf []byte, offset uint64, size uint32) {
	// Create a correct block info packet
	correctBlockInfo := &networking.CorrectBlockInfo{
		Offset: offset,
		Size: size,
		Data: buf,
	}

	// Wrap in a network packet
	correctBlockPacket := &networking.Packet{
		PacketType: networking.PacketTypeCorrectBlock,
		Payload: correctBlockInfo,
	}

	// Send the packet
	c.sendPacket(correctBlockPacket)
}

// handleHashPacket processes a hash verification packet
func (c *Client) handleHashPacket(packet *networking.Packet, wg *sync.WaitGroup) {
	defer wg.Done()

	// Extract hash info from packet
	hashInfo, ok := packet.Payload.(networking.HashInfo)
	if !ok {
		c.VerbosePrintln("invalid payload for type for HashInfo")
		c.InitiateCheckedReplication()
		return
	}

	// Read the block data from the device
	buf := make([]byte, hashInfo.Size)
	if _, err := c.UnderDevFile.ReadAt(buf, int64(hashInfo.Offset)); err != nil && err != io.EOF {
		c.VerbosePrintln("Error while hashing...")
		c.InitiateCheckedReplication()
		return
	}

	// Compute SHA-256 hash of the block
	shaWriter := simdsha256.New()
	shaWriter.Write(buf)
	hash := shaWriter.Sum(nil)

	// Compare hashes
	if !bytes.Equal(hash[:], hashInfo.Hash[:]) {
		c.DebugPrintln("Blocks are not equal")
		// Send the correct block data if hashes don't match
		c.sendCorrectBlock(buf, hashInfo.Offset, hashInfo.Size)
		return
	}

	c.DebugPrintln("Blocks are equal...")
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
func (c *Client) InitiateCheckedReplication() {
	// Reset buffer since we'll perform full verification
	c.ResetBufferIOCTL()

	// Pause the monitor to prevent interference during verification
	c.MonitorPauseContr.Pause()

	// Request hashes from server to verify consistency
	packet := networking.Packet{
		PacketType: networking.PacketTypeCmdGetHashes,
		Payload:    nil,
	}

	c.sendPacket(&packet)
}

// ProcessBufferInfo processes write operations stored in the buffer
func (c *Client) ProcessBufferInfo(bufferInfo *BufferInfo) {
	// Process each write operation in the buffer
	for i := uint64(0); i < bufferInfo.Length; i++ {
		// Skip if monitor is paused (during hash verification)
		if c.MonitorPauseContr.IsPaused() {
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

			// Read sector number
			if err = binary.Read(reader, binary.LittleEndian, &writeInfoPacket.Sector); err != nil {
				if terminated := c.CheckTermination(); terminated {
					c.VerbosePrintln("Terminating attempt to process buffer info...")
					return
				}
				c.VerbosePrintln("Failed to read Sector: ", err)
				RetrySleep()
				continue
			}

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
				PacketType: networking.PacketTypeWriteInfo,
				Payload:    writeInfoPacket,
			}

			c.sendPacket(&packet)

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
		c.serveIOCTL(BDR_CMD_READ_BUFFER_INFO, uintptr(unsafe.Pointer(&bufferInfo)))

		// Check if there are new writes to process
		newWrites := bufferInfo.HasNewWrites()
		if !newWrites {
			c.DebugPrintln("No information available, waiting...")
			time.Sleep(PollInterval * time.Millisecond)
			continue
		}

		// Check for buffer overflow
		overflow := bufferInfo.CheckOverflow()
		if overflow {
			c.VerbosePrintln("Buffer overflown...")

			// Wait until no new writes are occurring before starting verification
			for {
				if terminated := c.MonitorPauseContr.WaitIfPaused(c.TermChan); terminated {
					c.VerbosePrintln("Stopping change monitoring due to shutdown.")
					return
				}

				c.serveIOCTL(BDR_CMD_READ_BUFFER_INFO, uintptr(unsafe.Pointer(&bufferInfo)))

				newWrites = bufferInfo.HasNewWrites()
				if newWrites {
					// If new writes are occurring, wait before trying to verify
					RetrySleep()
					continue
				}

				break
			}

			// Initiate full device verification
			c.InitiateCheckedReplication()
			continue
		}

		// Process the write operations in the buffer
		c.ProcessBufferInfo(&bufferInfo)
	}
}

// handleHashing manages the hash verification process
func (c *Client) handleHashing(packet *networking.Packet) {
	c.DebugPrintln("Starting hashing phase...")
	var hashWg sync.WaitGroup
	defer c.MonitorPauseContr.Resume()
	defer hashWg.Wait()

	// Process the first hash packet
	hashWg.Add(1)
	go c.handleHashPacket(packet, &hashWg)

	// Process additional hash packets until complete
	for {
		packet := &networking.Packet{}
		sendHashReq := true
		c.receivePacket(packet, sendHashReq)

		if c.CheckTermination() {
			c.VerbosePrintln("Terminating hashing handler.")
			return
		}

		// Handle different packet types during hash verification
		switch packet.PacketType {
		case networking.PacketTypeErrInit:
			c.Println("ERROR: Remote and local devices do not have the same size.")
		case networking.PacketTypeHash:
			// Process each hash packet in parallel
			hashWg.Add(1)
			go c.handleHashPacket(packet, &hashWg)
		case networking.PacketTypeHashError:
			c.VerbosePrintln("ERROR: error occured on the remote side while hashing, retrying...")
		case networking.PacketTypeInfoHashingCompleted:
			c.VerbosePrintln("Hashing completed packet received")
			return
		default:
			c.VerbosePrintln("Unknown packet received while in hashing mode:", packet.PacketType)
		}
	}
}

// ListenPackets listens for incoming packets from the server
func (c *Client) ListenPackets(wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		packet := &networking.Packet{}
		sendHashReq := false
		c.receivePacket(packet, sendHashReq)

		if c.CheckTermination() {
			c.VerbosePrintln("Terminating packet listener.")
			return
		}

		// Handle different packet types
		switch packet.PacketType {
		case networking.PacketTypeErrInit:
			c.Println("ERROR: Remote and local devices do not have the same size.")
		case networking.PacketTypeHash:
			// Start hash verification mode
			c.handleHashing(packet)
		case networking.PacketTypeHashError:
			c.Println("ERROR: hash error accepted while not in hash mode")
		case networking.PacketTypeInfoHashingCompleted:
			c.DebugPrintln("ERROR: hashing completed packet accepted while not in hash mode")
		default:
			c.VerbosePrintln("Unknown packet received:", packet.PacketType)
		}
	}
}

// Run starts the client and handles graceful shutdown
func (c *Client) Run() {
	c.Println("Starting bdr client connected to", c.Config.IpAddress, "and port", c.Config.Port)

	// Start goroutines with wait group for clean shutdown
	var termWg sync.WaitGroup
	
	// Start change monitor goroutine
	termWg.Add(1)
	go c.MonitorChanges(&termWg)

	// Start packet listener goroutine
	termWg.Add(1)
	go c.ListenPackets(&termWg)

	// Set up signal handling for graceful shutdown
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt signal
	<-signalChan

	// Initiate graceful shutdown
	c.Println("Interrupt signal received. Shutting down...")
	close(c.TermChan)
	c.CloseClientConn()
	termWg.Wait()

	// Clean up resources
	c.CloseResources()
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
