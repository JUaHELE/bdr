package main

import (
	"bdr/benchmark"
	"bdr/journal"
	"bdr/networking"
	"bdr/utils"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	xxhash "github.com/zeebo/xxh3"
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
	WritePacketQueueSize   = 8 // number of writes that fit in the incoming queue; others wait
	CorrectPacketQueueSize = 8 // numbers are chosen not to overwhelm the disk
)

type State int

const (
	StateHashing State = iota
	StateWriting
	StateWritesToBuffet
)

// RetrySleep pauses execution for RetryInterval seconds
func RetrySleep() {
	time.Sleep(RetryInterval * time.Second)
}

// Server represents the main BDR (Block Device Replication) server
type Server struct {
	Config      *Config              // server configuration
	Listener    net.Listener         // TCP listener for incoming connections
	ClientInfo  *networking.InitInfo // information about the connected client
	Conn        net.Conn             // current client connection
	Encoder     *gob.Encoder         // encoder for outgoing data
	Decoder     *gob.Decoder         // decoder for incoming data
	TargetDevFd *os.File             // file descriptor for the target block device
	TermChan    chan struct{}        // channel for signaling termination
	Connected   bool                 // flag indicating if a client is connected
	ConnMutex   sync.Mutex           // mutex for thread-safe connection handling
	Journal     *journal.Journal
	Stats       *benchmark.BenchmarkStats

	stateMutex sync.Mutex
	state State
}

func (s State) String() string {
	return [...]string{
		"Hashing",
		"Writing",
		"WritesToBuffer",
	}[s]
}

func (s *Server) GetState() State {
	s.stateMutex.Lock()
	defer s.stateMutex.Unlock()
	return s.state
}

// SetState attempts to transition the client to a new state
func (s *Server) SetState(newState State) {
	s.stateMutex.Lock()
	defer s.stateMutex.Unlock()

	oldState := s.state
	s.state = newState

	s.VerbosePrintln("Client transitioned from", oldState, "to", newState, "\n")
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
		Stats:       benchmark.NewBenchmarkStats(cfg.Benchmark),
		state: StateWriting,
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

			if errors.Is(err, net.ErrClosed) {
				return
			}
			s.DebugPrintln("SendPacket failed: ", err)
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

	s.sendPacket(packet)
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
		hash   uint64
		err    error
	}

	type readTask struct {
		offset uint64
	}

	workChan := make(chan workItem, numWorkers)
	resultChan := make(chan resultItem, numWorkers)
	readTaskChan := make(chan readTask, numWorkers)

	readSize := uint64(hashedSpace)

	totalSize := s.ClientInfo.DeviceSize

	hashTimer := benchmark.NewTimer("Full disk hashing")
	totalBytesHashed := uint64(0)

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

	// Start a goroutine to read disk blocks and send to workers
	go func() {
		defer close(workChan)

		var wg sync.WaitGroup
		for i := 0; i < numWorkers; i++ {
			// Check for termination request
			wg.Add(1)
			go func() {
				defer wg.Done()
				for rTask := range readTaskChan {
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
				}
			}()
		}
		wg.Wait()
	}()

	// Launch worker goroutines
	go func() {
		defer close(resultChan)

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
					hash := xxhash.Hash(work.buffer)

					resultChan <- resultItem{
						offset: work.offset,
						size:   uint32(len(work.buffer)),
						hash:   hash,
						err:    nil,
					}
				}
			}()
		}
		wg.Wait()
	}()

	// Process results and send to client

	var sendWg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		sendWg.Add(1)
		go func() {
			defer sendWg.Done()
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
					s.DebugPrintln("Error while sending complete hashing info:", err)
					errPacket := networking.Packet{
						PacketType: networking.PacketTypeHashError,
					}
					s.sendPacket(&errPacket)
					return
				}

				totalBytesHashed += uint64(result.size)
			}
		}()
	}
	sendWg.Wait()

	elapsed := hashTimer.Stop()
	s.Stats.RecordHashing(elapsed, totalBytesHashed)

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

		state := s.GetState()

		if state == StateHashing {
			return
		} else if s.GetState() == StateWritesToBuffet {
			if err := s.WriteBufferWriteToJournal(&writeInfo); err != nil {
				s.VerbosePrintln("Can't copy write into the buffer", err)
				// TODO: solve this error
			}
		} else {
			// Calculate disk offset based on sector number and size
			dataToWrite := writeInfo.Data[:writeInfo.Size]

			s.Stats.RecordWrite(uint64(writeInfo.Size))

			// Write data to the target device
			if _, err := s.TargetDevFd.WriteAt(dataToWrite, int64(writeInfo.Offset)); err != nil {
				s.VerbosePrintln("Failed to write data to device:", err)
			}

			s.TargetDevFd.Sync()
		}
	}
}

func (s *Server) StartStatsPrinting(interval time.Duration) {
	if !s.Config.Benchmark {
		return
	}

	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				s.Stats.PrintStats()
			case <-s.TermChan:
				ticker.Stop()
				return
			}
		}
	}()
}

// WaitForInitInfo waits for and processes the initialization information from the client
func (s *Server) WaitForInitInfo() error {
	packet := &networking.Packet{}
	if err := s.Decoder.Decode(packet); err != nil {
		if err == io.EOF {
			return fmt.Errorf("connection closed by the client")
		}
		return fmt.Errorf("failed to decode packet: %v", err)
	}

	// Verify that we received the expected packet type
	if packet.PacketType != networking.PacketTypeInit {
		return fmt.Errorf("expected init packet, got: %d", packet.PacketType)
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
		s.Println("WARNING: Client has different size of the block device! Client: ", s.ClientInfo.DeviceSize, ", Server: ", deviceSize, "\n")
		return false
	} else {
		s.VerbosePrintln("Client is acceptable.")
	}
	return true
}

// handleCorrectPacket writes a correct block to the target device based on client information
func (s *Server) handleCorrectPacket(correctQueue chan *networking.Packet) {
	for packet := range correctQueue {

		// Extract correct block information
		correctInfo, ok := packet.Payload.(networking.CorrectBlockInfo)
		if !ok {
			s.VerbosePrintln("invalid packet type for correctblock")
		}

		for {
			// Write the correct data to disk
			if err := s.WriteCorrectBlockToJournal(&correctInfo); err != nil {
				s.VerbosePrintln("Can't write correct block:", err)
				RetrySleep()
				continue
			}
			break
		}
	}
}

func (s *Server) WriteJournalToReplica() {
	// TODO: set valid on journal
	s.VerbosePrintln("Writing journal to replica...")
	for i := uint64(0); i < s.Journal.CorrectOffset; {
		correctBlock, err := s.Journal.ReadCorrectBlock(i)
		if err != nil {
			s.VerbosePrintln("Can't write buffer write from journal to replica:", err)
			RetrySleep()
			continue
		}

		// Write the correct data to disk
		if _, err := s.TargetDevFd.WriteAt(correctBlock.Data, int64(correctBlock.Offset)); err != nil {
			s.VerbosePrintln("Can't write correct block")
			RetrySleep()
			continue
		}

		i++
	}

	for i := uint64(0); i < s.Journal.WriteOffset; {
		bufferWrite, err := s.Journal.ReadBufferWrite(i)
		if err != nil {
			s.VerbosePrintln("Can't write buffer write from journal to replica", err)
			RetrySleep()
			continue
		}


		dataToWrite := bufferWrite.Data[:bufferWrite.Size]

		// Write data to the target device
		if _, err := s.TargetDevFd.WriteAt(dataToWrite, int64(bufferWrite.Offset)); err != nil {
			s.VerbosePrintln("Failed to write data to device:", err)
		}

		s.TargetDevFd.Sync()

		i++
	}

	s.VerbosePrintln("Journal sucessfully written to replica.")
}

func (s *Server) CreateJournal() error {
	writeCount := s.ClientInfo.BufferByteSize / uint64(networking.WriteInfoSize)

	s.VerbosePrintln("Creating new journal at:", s.Config.JournalPath, "with buffer size in writes", writeCount, "and correct block size", networking.CorrectBlockByteSize, "...")
	jrn, err := journal.NewJournal(s.Config.JournalPath, s.ClientInfo.BufferByteSize, uint64(networking.WriteInfoSize), uint64(networking.CorrectBlockByteSize))
	if err != nil {
		return err
	}

	s.VerbosePrintln(jrn.String())

	err = jrn.Init()
	if err != nil {
		return err
	}

	s.Journal = jrn

	s.VerbosePrintln(s.ClientInfo.DeviceSize)
	cover := jrn.GetJournalCoverPercentage(s.ClientInfo.DeviceSize)
	s.VerbosePrintln("Journal created. Covers", cover, "percent of the target disk.")

	return err
}

// HandleClient manages a connected client session
func (s *Server) HandleClient(wg *sync.WaitGroup) {
	defer s.CloseClientConn()
	defer wg.Done()

	s.Println("Accepted connection from", s.Conn.RemoteAddr())

	var childWg sync.WaitGroup
	childWg.Wait()
	// Channel to signal hashing process termination
	hashingTermChan := make(chan struct{})

	// Queue for write operations
	writeQueue := make(chan *networking.Packet, WritePacketQueueSize)
	defer close(writeQueue)

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

	if err := s.CreateJournal(); err != nil {
		s.Println("Error occured while creating journal:", err)
		return
	}

	correctQueue := make(chan *networking.Packet, CorrectPacketQueueSize)
	defer close(correctQueue)

	childWg.Add(1)
	go func() {
		defer childWg.Done()
		s.handleCorrectPacket(correctQueue)
	}()

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
			s.DebugPrintln("Failed to decode packet:", err)
			continue
		}

		// Handle packet based on type
		switch packet.PacketType {
		case networking.PacketTypeCmdGetHashes:
			// Start a fresh hashing process
			close(hashingTermChan)
			for len(hashingTermChan) > 0 {
				time.Sleep(time.Millisecond * 10)
			}
			s.Journal.Reset()
			hashingTermChan = make(chan struct{})
			childWg.Add(1)
			go func() {
				defer childWg.Done()
				s.SetState(StateHashing)
				s.hashDiskAndSend(hashingTermChan, networking.HashedSpaceBase)
			}()
		case networking.PacketTypeWriteInfo:
			// Queue write operation
			writeQueue <- packet
		case networking.PacketTypeCorrectBlock:
			// Process correct block
			correctQueue <- packet
		case networking.PacketTypeInfoHashingCompleted:
			s.SetState(StateWritesToBuffet)
		case networking.PacketTypeBufferSent:
			for len(correctQueue) > 0 || len(writeQueue) > 0 {
				time.Sleep(time.Millisecond * 10)
			}
			RetrySleep()
			s.WriteJournalToReplica()
			s.SetState(StateWriting)
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

func (s *Server) WriteCorrectBlockToJournal(correctBlock *networking.CorrectBlockInfo) error {
	s.DebugPrintln("Writing correct block to journal on offset", s.Journal.CorrectOffset)
	err := s.Journal.WriteCorrectBlock(s.Journal.CorrectOffset, correctBlock)
	if err != nil {
		return err
	}

	s.Journal.CorrectOffset++

	return nil
}

func (s *Server) WriteBufferWriteToJournal(bufferWrite *networking.WriteInfo) error {
	s.DebugPrintln("Writing buffer write to journal on offset", s.Journal.WriteOffset)

	err := s.Journal.WriteBufferWrite(s.Journal.WriteOffset, bufferWrite)
	if err != nil {
		return err
	}

	s.Journal.WriteOffset++

	return nil
}

func (s *Server) WriteCorrectBlockToReplica(correctBlock *networking.CorrectBlockInfo) {
	for {
		// Write the correct data to disk
		if _, err := s.TargetDevFd.WriteAt(correctBlock.Data, int64(correctBlock.Offset)); err != nil {
			s.VerbosePrintln("Can't write correct block")
			RetrySleep()
			continue
		}
		break
	}
}

func (s *Server) WriteBufferWriteToReplica(bufferWrite *networking.WriteInfo) {
	for {
		// Write the correct data to disk
		dataToWrite := bufferWrite.Data[:bufferWrite.Size]

		// Write data to the target device
		if _, err := s.TargetDevFd.WriteAt(dataToWrite, int64(bufferWrite.Offset)); err != nil {
			s.VerbosePrintln("Failed to write data to device:", err)
		}

		s.TargetDevFd.Sync()
		break
	}
}


func (s *Server) CopyJournalToReplica(jrn *journal.Journal) {
	for i := uint64(0); i < jrn.CorrectOffset; {
		correctBlock, err := jrn.ReadCorrectBlock(i)
		if err != nil {
			s.Println("Can't copy journal to replica:", err)
			RetrySleep()
			continue
		}

		s.WriteCorrectBlockToReplica(correctBlock)
		i++
	}

	s.TargetDevFd.Sync()

	for i := uint64(0); i < jrn.WriteOffset; {
		correctBlock, err := jrn.ReadBufferWrite(i)
		if err != nil {
			s.Println("Can't copy journal to replica:", err)
			RetrySleep()
			continue
		}

		s.WriteBufferWriteToReplica(correctBlock)
		i++
	}
}

func (s *Server) CheckJournal() {
	jrn, err := journal.OpenJournal(s.Config.JournalPath)
	if err != nil {
		s.DebugPrintln("There is corrupted journal on specified path, continuing...")
		return
	}

	if !jrn.IsValid() {
		s.DebugPrintln("Journal on specified path is invalid, continuing...")
		s.DebugPrintln(jrn.String())
		return
	}

	s.Println("Journal on specified path is valid, copying the journal to replica...")
	s.VerbosePrintln(jrn.String())

	s.CopyJournalToReplica(jrn)
}

// Run starts the server and handles termination signals
func (s *Server) Run() {
	s.Println("Starting bdr server listening on", s.Config.IpAddress, "and port", s.Config.Port)
	s.VerbosePrintln("Target device:", s.Config.TargetDevPath)

	s.CheckJournal()

	var termWg sync.WaitGroup
	termWg.Add(1)
	go s.HandleConnections(&termWg)

	s.StartStatsPrinting(10 * time.Second)

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
	err := ValidateArgs(&cfg.TargetDevPath, &cfg.Port, &cfg.IpAddress, &cfg.JournalPath)
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
