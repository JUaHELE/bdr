// server

package main

import (
	"bdr/networking"
	"bdr/utils"
	"encoding/gob"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"time"
	"syscall"
	"io"
	"runtime"

	simdsha256 "github.com/minio/sha256-simd"
)

// Connection and timing constants
const (
	PollInterval  = 100 // in milliseconds - polling frequency
	RetryInterval = 1   // seconds - time to wait between retries
)

const (
	ConnectionTimeout = 30 * time.Second // maximum time to wait for connection
	ReconnectDelay    = 5 * time.Second  // time to wait before attempting to reconnect
)

const (
	WritePacketQueueSize = 100 // number of writes that fit in the incoming queue; others wait
)

// RetrySleep pauses execution for RetryInterval seconds
func RetrySleep() {
	time.Sleep(RetryInterval * time.Second)
}

// Server represents the main BDR (Block Device Replication) server
type Server struct {
	Config      *Config                // server configuration
	Listener    net.Listener           // TCP listener for incoming connections
	ClientInfo  *networking.InitInfo   // information about the connected client
	Conn        net.Conn               // current client connection
	Encoder     *gob.Encoder           // encoder for outgoing data
	Decoder     *gob.Decoder           // decoder for incoming data
	TargetDevFd *os.File               // file descriptor for the target block device
	TermChan    chan struct{}          // channel for signaling termination
	Connected   bool                   // flag indicating if a client is connected
	ConnMutex   sync.Mutex             // mutex for thread-safe connection handling
}

// Println logs a message with standard priority
func (s *Server) Println(args ...interface{}) {
	s.Config.Println(args...)
}

// VerbosePrintln logs a message with verbose priority
func (s *Server) VerbosePrintln(args ...interface{}) {
	s.Config.VerbosePrintln(args...)
}

// DebugPrintln logs a message with debug priority
func (s *Server) DebugPrintln(args ...interface{}) {
	s.Config.DebugPrintln(args...)
}

// NewServer creates and initializes a new server instance
func NewServer(cfg *Config) (*Server, error) {
	// Open the target block device with read/write access
	targetDeviceFd, err := os.OpenFile(cfg.TargetDevPath, os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open device: %w", err)
	}

	// Create and start the TCP listener
	address := fmt.Sprintf("%s:%d", cfg.IpAddress, cfg.Port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		targetDeviceFd.Close()
		return nil, fmt.Errorf("failed to listen on port %d: %w", cfg.Port, err)
	}

	// Initialize the server structure
	server := &Server{
		Config:      cfg,
		Listener:    listener,
		TargetDevFd: targetDeviceFd,
		TermChan:    make(chan struct{}),
		Connected:   false,
	}

	return server, nil
}

// sendPacket sends a network packet, retrying until successful or terminated
func (s *Server) sendPacket(packet *networking.Packet) {
	for {
		err := s.Encoder.Encode(packet)
		if err != nil {
			// Check if we should stop trying due to termination signal
			if terminated := s.CheckTermination(); terminated {
				s.VerbosePrintln("Terminating attempt for successful packet send...")
				return
			}

			s.VerbosePrintln("SendPacket failed: ", err)
			RetrySleep()
			continue
		}
		break
	}
}

// receivePacket receives a network packet, retrying until successful or terminated
func (s *Server) receivePacket(packet *networking.Packet) {
	for {
		err := s.Decoder.Decode(packet)
		if err != nil {
			// Check if we should stop trying due to termination signal
			if terminated := s.CheckTermination(); terminated {
				s.VerbosePrintln("Terminating attempt for successful packet send...")
				return
			}

			s.VerbosePrintln("receivePacket failed: ", err)
			RetrySleep()
			continue
		}
		break
	}
}

// CompleteHashing sends a notification that the hashing process is complete
func (s *Server) CompleteHashing() {
	packet := &networking.Packet{
		PacketType: networking.PacketTypeInfoHashingCompleted,
		Payload:    nil,
	}

	err := s.Encoder.Encode(packet)
	if err != nil {
		s.VerbosePrintln("Error while sending complete hashing info:", err)
		return
	}

	s.VerbosePrintln("Hashing completed.")
}

// hashDiskAndSend reads the disk in chunks, computes SHA-256 hashes, and sends them to the client
func (s *Server) hashDiskAndSend(termChan chan struct{}, hashedSpace uint64) {
	s.VerbosePrintln("Hashing disk...")
	
	// Number of worker goroutines to use
	numWorkers := runtime.NumCPU()
	
	// Create work and result channels
	type workItem struct {
		offset uint64
		buffer []byte
	}
	type resultItem struct {
		offset uint64
		size   uint32
		hash   []byte
		err    error
	}
	
	workChan := make(chan workItem, numWorkers)
	resultChan := make(chan resultItem, numWorkers)
	doneChan := make(chan struct{})

	readSize := uint64(hashedSpace)

	totalSize := s.ClientInfo.DeviceSize
	
	// Launch worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for work := range workChan {
				// Check for termination
				if utils.ChanHasTerminated(termChan) {
					return
				}
				
				// Compute SHA-256 hash using SIMD-accelerated implementation
				shaWriter := simdsha256.New()
				shaWriter.Write(work.buffer)
				hash := shaWriter.Sum(nil)
				
				resultChan <- resultItem{
					offset: work.offset,
					size:   uint32(len(work.buffer)),
					hash:   hash,
					err:    nil,
				}
			}
		}()
	}
	
	// Start a goroutine to close resultChan when all workers are done
	go func() {
		wg.Wait()
		close(resultChan)
		close(doneChan)
	}()
	

	type readTask struct {
		offset uint64
	}

	readTaskChan := make(chan readTask, numWorkers)
	// Start a goroutine to read disk blocks and send to workers
	go func() {
		defer close(workChan)

		var wg sync.WaitGroup
		for rTask := range readTaskChan{
			// Check for termination request
			wg.Add(1)
			go func() {
				defer wg.Done()
				if utils.ChanHasTerminated(termChan) {
					return
				}
				
				// Create a new buffer for each read to avoid data races
				buf := make([]byte, readSize)
				
				// Read a block from the disk
				n, err := s.TargetDevFd.ReadAt(buf, int64(rTask.offset))
				if err != nil && err != io.EOF {
					resultChan <- resultItem{
						offset: rTask.offset,
						err:    err,
					}
					return
				}
				
				// End of disk reached
				if n == 0 {
					return
				}
				
				// Adjust buffer size if we read less than expected
				if n < len(buf) {
					buf = buf[:n]
				}
				
				// Send work to a worker
				workChan <- workItem{
					offset: rTask.offset,
					buffer: buf,
				}
			}()
		}
		wg.Wait()
	}()

	go func() {
		for offset := uint64(0); offset < totalSize; offset += readSize {
			// Check for termination request before queuing more work
			if utils.ChanHasTerminated(termChan) {
			    break
			}
			readTaskChan <- readTask{offset: offset}
		}

		close(readTaskChan)
	}()
	
	// Process results and send to client
	for result := range resultChan {
		// Check for termination request
		if utils.ChanHasTerminated(termChan) {
			s.Println("Hashing has terminated!")
			return
		}
		
		// Handle errors
		if result.err != nil {
			s.VerbosePrintln("Error while hashing disk:", result.err)
			// Notify client about the error
			errPacket := networking.Packet{
				PacketType: networking.PacketTypeHashError,
			}
			s.sendPacket(&errPacket)
			return
		}
		
		// Create hash information packet
		hashInfo := networking.HashInfo{
			Offset: result.offset,
			Size:   result.size,
			Hash:   result.hash,
		}
		hashPacket := networking.Packet{
			PacketType: networking.PacketTypeHash,
			Payload:    hashInfo,
		}
		
		// Send hash to client
		if err := s.Encoder.Encode(&hashPacket); err != nil {
			s.VerbosePrintln("Error while sending complete hashing info:", err)
			errPacket := networking.Packet{
				PacketType: networking.PacketTypeHashError,
			}
			s.sendPacket(&errPacket)
			return
		}
	}
	
	// Wait for everything to finish
	<-doneChan
	
	// Notify client that hashing is complete
	s.CompleteHashing()
}

// CheckTermination checks if termination has been requested
func (s *Server) CheckTermination() bool {
	select {
	case <-s.TermChan:
		return true
	default:
		return false
	}
}

// Close cleanly shuts down all server resources
func (s *Server) Close() {
	s.ConnMutex.Lock()
	defer s.ConnMutex.Unlock()

	// Close the client connection if it exists
	if s.Conn != nil {
		s.Conn.Close()
		s.Conn = nil
	}
	// Close the listener if it exists
	if s.Listener != nil {
		s.Listener.Close()
	}
	// Close the device file descriptor if it exists
	if s.TargetDevFd != nil {
		s.TargetDevFd.Close()
	}
}

// CloseClientConn safely closes the client connection
func (s *Server) CloseClientConn() {
	s.ConnMutex.Lock()
	defer s.ConnMutex.Unlock()

	s.Connected = false
	if s.Conn != nil {
		s.Conn.Close()
		s.Conn = nil
	}

	s.Encoder = nil
	s.Decoder = nil
}

// handleWriteInfoPacket processes write requests from the client
func (s *Server) handleWriteInfoPacket(writeChan chan *networking.Packet) {
	for packet := range writeChan {
		s.DebugPrintln("Write information packet received.")
		
		// Extract write information from the packet
		writeInfo, ok := packet.Payload.(networking.WriteInfo)
		if !ok {
			s.VerbosePrintln("invalid payload type for WriteInfo")
		}

		// Calculate disk offset based on sector number and size
		writeOffset := int64(writeInfo.Sector) * int64(s.ClientInfo.SectorSize)
		dataToWrite := writeInfo.Data[:writeInfo.Size]

		// Write data to the target device
		if _, err := s.TargetDevFd.WriteAt(dataToWrite, writeOffset); err != nil {
			s.VerbosePrintln("Failed to write data to device:", err)
		}
	}
}

// WaitForInitInfo waits for and processes the initialization information from the client
func (s *Server) WaitForInitInfo() error {
	packet := &networking.Packet{}
	if err := s.Decoder.Decode(packet); err != nil {
		if err == io.EOF {
			return fmt.Errorf("Connection closed by the client.")
		}
		return fmt.Errorf("Failed to decode packet: %v", err)
	}

	// Verify that we received the expected packet type
	if packet.PacketType != networking.PacketTypeInit {
		return fmt.Errorf("Expected init packet, got: %d", packet.PacketType)
	}

	// Extract client information
	initInfo, ok := packet.Payload.(networking.InitInfo)
	if !ok {
		return fmt.Errorf("invalid payload type for init packet")
	}

	s.ClientInfo = &initInfo
	s.VerbosePrintln("Init info received...")

	return nil
}

// CheckValidSizes verifies that the client and server devices have the same size
func (s *Server) CheckValidSizes() bool {
	deviceSize, err := utils.GetDeviceSize(s.Config.TargetDevPath)
	if err != nil || deviceSize != s.ClientInfo.DeviceSize {
		// Send error packet to client
		errPacket := networking.Packet{
			PacketType: networking.PacketTypeErrInit,
			Payload:    nil,
		}

		err := s.Encoder.Encode(errPacket)
		if err != nil {
			s.VerbosePrintln("Error when sending errInit packet")
		}
		s.Println("WARNING: Client has different size of the block device!")
		return false
	} else {
		s.VerbosePrintln("Client is acceptable.")
	}
	return true
}

// handleCorrectPacket writes a correct block to the target device based on client information
func (s *Server) handleCorrectPacket(packet *networking.Packet) {
	s.DebugPrintln("Writing correct block...")

	// Extract correct block information
	correctInfo, ok := packet.Payload.(networking.CorrectBlockInfo)
	if !ok {
		s.VerbosePrintln("invalid packet type for correctblock")
	}
	
	// Write the correct data to disk
	if _, err := s.TargetDevFd.WriteAt(correctInfo.Data, int64(correctInfo.Offset)); err != nil {
		s.VerbosePrintln("Can't write correct block")
		// TODO: solve this - maybe ask for it again
	}
}

// HandleClient manages a connected client session
func (s *Server) HandleClient(wg *sync.WaitGroup) {
	defer s.CloseClientConn()
	defer wg.Done()

	s.Println("Accepted connection from", s.Conn.RemoteAddr())

	// Channel to signal hashing process termination
	hashingTermChan := make(chan struct{})

	// Queue for write operations
	writeQueue := make(chan *networking.Packet, WritePacketQueueSize)
	defer close(writeQueue)

	var childWg sync.WaitGroup

	// Start a goroutine to handle write operations
	childWg.Add(1)
	go func() {
		defer childWg.Done()
		s.handleWriteInfoPacket(writeQueue)
	}()

	// Wait for and process client initialization information
	if err := s.WaitForInitInfo(); err != nil {
		s.Println("Error occurred while waiting on init packet:", err)
		return
	}

	// Verify device sizes match
	if valid := s.CheckValidSizes(); !valid {
		return
	}

	// Main packet processing loop
	for {
		if s.CheckTermination() {
			s.VerbosePrintln("Terminating client handler.")
			return
		}

		// Receive next packet
		packet := &networking.Packet{}
		if err := s.Decoder.Decode(packet); err != nil {
			if err == io.EOF {
				s.Println("Connection closed by the client.")
				return
			}
			s.VerbosePrintln("Failed to decode packet:", err)
			continue
		}

		// Handle packet based on type
		switch packet.PacketType {
		case networking.PacketTypeCmdGetHashes:
			// Start a fresh hashing process
			close(hashingTermChan)
			hashingTermChan = make(chan struct{})
			childWg.Add(1)
			go func() {
				defer childWg.Done()
				s.hashDiskAndSend(hashingTermChan, networking.HashedSpaceBase)
			}()
		case networking.PacketTypeWriteInfo:
			// Queue write operation
			writeQueue <- packet
		case networking.PacketTypeCorrectBlock:
			// Process correct block
			s.DebugPrintln("Correct block arrived")
			childWg.Add(1)
			go func() {
				defer childWg.Done()
				s.handleCorrectPacket(packet)
			}()
		default:
			s.VerbosePrintln("Unknown packet received:", packet.PacketType)
		}
	}
}

// HandleConnections manages incoming connection requests
func (s *Server) HandleConnections(wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		// Check for termination request
		if s.CheckTermination() {
			s.VerbosePrintln("Terminating connection listener.")
			return
		}

		// Accept new connection
		conn, err := s.Listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				s.VerbosePrintln("Listener closed. Terminating...")
				break
			}

			s.Println("Listener.Accept error:", err)
			continue
		}

		// Ensure only one client is connected at a time
		s.ConnMutex.Lock()
		if s.Connected {
			conn.Close()
			s.ConnMutex.Unlock()
			time.Sleep(ReconnectDelay)
			continue
		}

		// Set up connection and encoders/decoders
		s.Conn = conn
		s.Encoder = gob.NewEncoder(conn)
		s.Decoder = gob.NewDecoder(conn)
		s.Connected = true

		s.ConnMutex.Unlock()

		// Start client handler
		var clientWg sync.WaitGroup
		clientWg.Add(1)

		go s.HandleClient(&clientWg)
		clientWg.Wait()

		s.VerbosePrintln("Client disconnected.")
	}
}

// Run starts the server and handles termination signals
func (s *Server) Run() {
	s.Println("Starting bdr server listening on", s.Config.IpAddress, "and port", s.Config.Port)
	s.VerbosePrintln("Target device:", s.Config.TargetDevPath)

	var termWg sync.WaitGroup
	termWg.Add(1)
	go s.HandleConnections(&termWg)

	// Set up signal handling for graceful shutdown
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	<-signalChan

	s.Println("Interrupt signal received. Shutting down...")
	close(s.TermChan) // Signal termination to all goroutines

	s.Listener.Close()
	termWg.Wait()
	s.Close()

	s.Println("bdr server terminated successfully.")
}

// main initializes and runs the server
func main() {
	// Initialize configuration
	cfg := NewConfig()

	// Validate command-line arguments
	err := ValidateArgs(&cfg.TargetDevPath, &cfg.Port, &cfg.IpAddress)
	if err != nil {
		log.Fatalf("Invalid arguments: %v", err)
	}

	// Create and initialize server
	server, err := NewServer(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}
	defer server.Close()

	// Register the packet types for the gob encoder/decoder
	networking.RegisterGobPackets()

	// Start the server
	server.Run()
}
