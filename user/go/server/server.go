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
	PollInterval           = 100 // in milliseconds - polling frequency
	RetryInterval          = 1   // seconds - time to wait between retries
	BenchmarkPrintInterval = 10
)

const (
	ConnectionTimeout = 30 * time.Second // maximum time to wait for connection
	ReconnectDelay    = 5 * time.Second  // time to wait before attempting to reconnect
)

const (
	WritePacketQueueSize   = 8 // number of writes that fit in the incoming queue; others wait
	CorrectPacketQueueSize = 8 // numbers are chosen not to overwhelm the disk
	JournalPacketQueueSize = 8
)

type State int

const (
	StateHashing State = iota
	StateWriting
	StateDisconnected
	StateDestroyingReplica
	StateJournalOverflow
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
	state      State
}

func (s State) String() string {
	return [...]string{
		"Hashing",
		"Writing",
		"Disconnected",
		"DestroyingReplica",
		"JournalOverflow",
	}[s]
}

func WaitUntilChannelEmpty(ch chan *networking.Packet) {
    for {
        if len(ch) == 0 {
            return
        }
        time.Sleep(10 * time.Millisecond)
    }
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

	s.VerbosePrintln("Server transitioned from", oldState, "to", newState)
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

func (s *Server) InitiateShutdown() {
	select {
	case <-s.TermChan:
		// already closed
	default:
		close(s.TermChan)
	}
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
		state:       StateWriting,
	}

	return server, nil
}

// sendPacket sends a network packet, retrying until successful or terminated
func (s *Server) SendPacket(packet *networking.Packet) error {
	err := s.Encoder.Encode(packet)
	if err != nil {
		// Check if we should stop trying due to termination signal
		return fmt.Errorf("SendPacket failed: %v", err)
	}

	return nil
}

func CreateInfoPacket(packetType uint32) *networking.Packet {
	packet := &networking.Packet{
		PacketType: packetType,
		Payload:    nil,
	}

	return packet
}

// hashDiskAndSend reads the disk in chunks, computes hashes, and sends them to the client
func (s *Server) HashDisk(termChan chan struct{}, hashedSpace uint64, wg *sync.WaitGroup) {
	defer wg.Done()

	state := s.GetState()

	if state != StateDestroyingReplica {
		s.VerbosePrintln("Reseting journal...")
		s.Journal.ResetTo(s.Journal.WriteOffset, s.Journal.CorrectOffset)
	}

	s.VerbosePrintln("Hashing disk...")

	packet := CreateInfoPacket(networking.PacketTypeInfoStartHashing)
	err := s.SendPacket(packet)
	if err != nil {
		if utils.IsConnectionClosed(err) {
			return
		}
		s.VerbosePrintln("Can't send starting completion packet:", err)
	}

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
					errPacket := CreateInfoPacket(networking.PacketTypeErrHash)
					s.SendPacket(errPacket)
				}

				// Create hash information packet
				hashInfo := networking.HashInfo{
					Offset: result.offset,
					Size:   result.size,
					Hash:   result.hash,
				}
				hashPacket := networking.Packet{
					PacketType: networking.PacketTypePayloadHash,
					Payload:    hashInfo,
				}

				// Send hash to client
				if err := s.Encoder.Encode(&hashPacket); err != nil {
					if utils.IsConnectionClosed(err) {
						return
					}

					s.DebugPrintln("Error while sending hashing info:", err)
					errPacket := CreateInfoPacket(networking.PacketTypeErrHash)
					s.SendPacket(errPacket)
				}

				totalBytesHashed += uint64(result.size)
			}
		}()
	}
	sendWg.Wait()

	elapsed := hashTimer.Stop()
	s.Stats.RecordHashing(elapsed, totalBytesHashed)

	infoPacket := CreateInfoPacket(networking.PacketTypeInfoHashingCompleted)
	err = s.SendPacket(infoPacket)
	if err != nil {
		if utils.IsConnectionClosed(err) {
			return
		}

		s.VerbosePrintln("Can't send hashing completion packet:", err)
	}
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
	if packet.PacketType != networking.PacketTypeInfoInit {
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
func (s *Server) CheckValidSizes() error {
	deviceSize, err := utils.GetDeviceSize(s.Config.TargetDevPath)
	if err != nil || deviceSize != s.ClientInfo.DeviceSize {
		// Send error packet to client
		return fmt.Errorf("Client has different size of the block device")
	}
	return nil
}

// handleCorrectPacket writes a correct block to the target device based on client information
func (s *Server) HandleCorrectPackets(correctQueue chan *networking.Packet, wg *sync.WaitGroup) {
	wg.Done()

	for packet := range correctQueue {
		// Extract correct block information
		correctInfo, ok := packet.Payload.(networking.CorrectBlockInfo)
		if !ok {
			s.VerbosePrintln("invalid packet type for correctblock")
		}

		// Write the correct data to disk
		err := s.WriteCorrectBlockToReplica(&correctInfo)
		if err != nil {
			s.VerbosePrintln("Can't write block to replica:", err)
		}
	}
}

func (s *Server) HandleWritePackets(writeQueue chan *networking.Packet, wg *sync.WaitGroup) {
	wg.Done()

	for packet := range writeQueue {
		state := s.GetState()
		if state != StateWriting {
			continue
		}

		s.DebugPrintln("Write information packet received.")

		// Extract write information from the packet
		writeInfo, ok := packet.Payload.(networking.WriteInfo)
		if !ok {
			s.VerbosePrintln("invalid payload type for WriteInfo")
		}

		// Calculate disk offset based on sector number and size
		dataToWrite := writeInfo.Data[:writeInfo.Size]

		s.Stats.RecordWrite(uint64(writeInfo.Size))

		// Write data to the target device
		for {
			if _, err := s.TargetDevFd.WriteAt(dataToWrite, int64(writeInfo.Offset)); err != nil {
				s.VerbosePrintln("Failed to write data to device:", err)
				continue
			}
			break
		}
		s.TargetDevFd.Sync()
	}
}

func (s *Server) HandleJournalBufferWrite(packet *networking.Packet) error {
	writeInfo, ok := packet.Payload.(networking.WriteInfo)
	if !ok {
		return fmt.Errorf("Invalid payload type for WriteInfo")
	}

	err := s.WriteBufferWriteToJournal(&writeInfo)
	if err != nil {
		return fmt.Errorf("Can't write buffer write to replica: %v", err)
	}

	return nil
}

func (s *Server) HandleJournalCorrectBlock(packet *networking.Packet) error {
	correctInfo, ok := packet.Payload.(networking.CorrectBlockInfo)
	if !ok {
		return fmt.Errorf("invalid packet type for CorrectBlockInfo")
	}

	err := s.WriteCorrectBlockToJournal(&correctInfo)
	if err != nil {
		return fmt.Errorf("Can't write buffer write to replica: %v", err)
	}

	return nil
}

func (s *Server) HandleJournalPackets(journalQueue chan *networking.Packet, wg *sync.WaitGroup) {
	defer wg.Done()

	for packet := range journalQueue {
		state := s.GetState()
		if state == StateJournalOverflow {
			continue
		}

		switch packet.PacketType {
		case networking.PacketTypePayloadBufferWrite:
			err := s.HandleJournalBufferWrite(packet)
			if err != nil {
				errPacket := CreateInfoPacket(networking.PacketTypeErrJournalOverflow)
				errSend := s.SendPacket(errPacket)
				if errSend != nil {
					s.VerbosePrintln("Can't send journal overflow packet", errSend)
				}

				s.VerbosePrintln("Journal packet handle failed:", err)
				s.SetState(StateJournalOverflow)
			}
		case networking.PacketTypePayloadCorrectBlock:
			err := s.HandleJournalCorrectBlock(packet)
			if err != nil {
				errPacket := CreateInfoPacket(networking.PacketTypeErrJournalOverflow)
				errSend := s.SendPacket(errPacket)
				if errSend != nil {
					s.VerbosePrintln("Can't send journal overflow packet", errSend)
				}

				s.VerbosePrintln("Journal packet handle failed:", err)
				s.SetState(StateJournalOverflow)
			}
		}
	}
}

func (s *Server) HandleJournalWriting() {
	state := s.GetState()

	if state == StateDestroyingReplica {
		return
	}

	err := s.WriteJournalToReplica()
	if err != nil {
		s.VerbosePrintln("Can't write journal to replica:", err)

		errPacket := CreateInfoPacket(networking.PacketTypeErrJournalOverflow)
		errSend := s.SendPacket(errPacket)
		if err != nil {
			s.VerbosePrintln("Can't send journal overflow packet:", errSend)
		}
	}
}

func (s *Server) WriteJournalToReplica() error {
	s.Journal.Validate()

	s.VerbosePrintln("Writing journal to replica...")
	s.VerbosePrintln("Writing correct blocks...")
	for i := uint64(0); i < s.Journal.CorrectOffset; {
		correctBlock, err := s.Journal.ReadCorrectBlock(i)
		if err != nil {
			return fmt.Errorf("Can't read correct block from journal: %v", err)
		}

		// Write the correct data to disk
		if _, err := s.TargetDevFd.WriteAt(correctBlock.Data, int64(correctBlock.Offset)); err != nil {
			return fmt.Errorf("Can't write correct block: %v", err)
		}

		i++
	}
	s.TargetDevFd.Sync()

	s.VerbosePrintln("Writing buffer writes...")
	for i := uint64(0); i < s.Journal.WriteOffset; {
		bufferWrite, err := s.Journal.ReadBufferWrite(i)
		if err != nil {
			return fmt.Errorf("Can't read buffer write from journal: %v", err)
		}

		dataToWrite := bufferWrite.Data[:bufferWrite.Size]

		// Write data to the target device
		if _, err := s.TargetDevFd.WriteAt(dataToWrite, int64(bufferWrite.Offset)); err != nil {
			return fmt.Errorf("Can't write buffer write: %v", err)
		}

		s.TargetDevFd.Sync()

		i++
	}

	err := s.Journal.Invalidate()
	if err != nil {
		return fmt.Errorf("Can't invalidate the journal: %v", err)
	}

	s.VerbosePrintln("Journal sucessfully written to replica.")

	return nil
}

func (s *Server) CreateJournal() error {
	writeCount := s.ClientInfo.BufferByteSize / uint64(networking.WriteInfoSize)

	s.VerbosePrintln("Creating new journal at:", s.Config.JournalPath, "with buffer size in writes", writeCount, "and correct block size", networking.CorrectBlockByteSize, "...")
	jrn, err := journal.NewJournal(s.Config.JournalPath, s.ClientInfo.BufferByteSize, uint64(networking.WriteInfoSize), uint64(networking.CorrectBlockByteSize))
	if err != nil {
		return err
	}

	s.VerbosePrintln(jrn.String())

	err = jrn.InitWithoutClearing()
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
	defer wg.Done()
	defer s.CloseClientConn()
	defer s.SetState(StateDisconnected)

	s.SetState(StateWriting)
	s.Println("Accepted connection from", s.Conn.RemoteAddr())

	if err := s.WaitForInitInfo(); err != nil {
		s.VerbosePrintln("Error occured while waiting for init info:", err)

		errPacket := CreateInfoPacket(networking.PacketTypeErrInit)
		err := s.SendPacket(errPacket)
		if err != nil {
			s.VerbosePrintln("Can't send PacketTypeErrInit packet:", err)
		}
		return
	}

	// Verify device sizes match
	if err := s.CheckValidSizes(); err != nil {
		s.Println("Source device and replica don't have the same size")

		errPacket := CreateInfoPacket(networking.PacketTypeErrInvalidSizes)
		err := s.SendPacket(errPacket)
		if err != nil {
			s.VerbosePrintln("Can't send init info packet:", err)
		}
		return
	}

	if err := s.CreateJournal(); err != nil {
		s.Println("Can't create journal:", err)
		errPacket := CreateInfoPacket(networking.PacketTypeErrJournalCreate)
		err := s.SendPacket(errPacket)
		if err != nil {
			s.VerbosePrintln("Can't send create journal packet:", err)
		}
		return
	}

	var childWg sync.WaitGroup
	defer childWg.Wait()

	var hashWg sync.WaitGroup
	defer hashWg.Wait()

	hashTermChan := make(chan struct{})

	// QueueWriteBufferWriteToReplica for write operations
	writeQueue := make(chan *networking.Packet, WritePacketQueueSize)
	defer close(writeQueue)

	childWg.Add(1)
	go s.HandleWritePackets(writeQueue, &childWg)

	var journalWg sync.WaitGroup
	defer journalWg.Wait()

	journalQueue := make(chan *networking.Packet, JournalPacketQueueSize)

	correctQueue := make(chan *networking.Packet, CorrectPacketQueueSize)
	defer close(correctQueue)

	childWg.Add(1)
	go s.HandleCorrectPackets(correctQueue, &childWg)

	for {
		if s.CheckTermination() {
			s.VerbosePrintln("Terminating client handler.")
			close(hashTermChan)
			close(journalQueue)
			return
		}

		// Receive next packet
		packet := &networking.Packet{}
		if err := s.Decoder.Decode(packet); err != nil {
			if utils.IsConnectionClosed(err) {
				close(hashTermChan)
				close(journalQueue)
				return
			}
			s.DebugPrintln("Failed to decode packet:", err)
			continue
		}

		// Handle packet based on type
		switch packet.PacketType {
		case networking.PacketTypeCmdStartHashing:
			s.VerbosePrintln("Starting hashing...")

			s.SetState(StateHashing)

			close(journalQueue)
			journalWg.Wait()

			journalQueue = make(chan *networking.Packet, JournalPacketQueueSize)
			journalWg.Add(1)
			go s.HandleJournalPackets(journalQueue, &journalWg)
			
			close(hashTermChan)
			hashWg.Wait()

			hashTermChan = make(chan struct{})
			hashWg.Add(1)
			go s.HashDisk(hashTermChan, networking.HashedSpaceBase, &hashWg)
		case networking.PacketTypePayloadBufferWrite:
			state := s.GetState()

			if state == StateHashing {
				journalQueue <- packet
			} else if state == StateWriting {
				writeQueue <- packet
			}
		case networking.PacketTypePayloadCorrectBlock:
			state := s.GetState()

			if state == StateHashing {
				journalQueue <- packet
			} else if state == StateDestroyingReplica {
				correctQueue <- packet
			}
		case networking.PacketTypeInfoHashingCompleted:
			s.VerbosePrintln("Hashing completed.")

			close(journalQueue)
			journalWg.Wait()
			journalQueue = make(chan *networking.Packet, JournalPacketQueueSize)

			s.HandleJournalWriting()
			s.SetState(StateWriting)
		case networking.PacketTypeCmdStartFullReplication:
			s.VerbosePrintln("Starting full replication.")
			s.SetState(StateDestroyingReplica)

			close(hashTermChan)
			hashWg.Wait()

			hashTermChan = make(chan struct{})
			hashWg.Add(1)
			go s.HashDisk(hashTermChan, networking.HashedSpaceBase, &hashWg)
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

func (s *Server) WriteCorrectBlockToReplica(correctBlock *networking.CorrectBlockInfo) error {
	// Write the correct data to disk
	if _, err := s.TargetDevFd.WriteAt(correctBlock.Data, int64(correctBlock.Offset)); err != nil {
		return fmt.Errorf("Failed to write buffer write to replica: %v", err)
	}

	return nil
}

func (s *Server) WriteBufferWriteToReplica(bufferWrite *networking.WriteInfo) error {
	// Write the correct data to disk
	dataToWrite := bufferWrite.Data[:bufferWrite.Size]

	// Write data to the target device
	if _, err := s.TargetDevFd.WriteAt(dataToWrite, int64(bufferWrite.Offset)); err != nil {
		return fmt.Errorf("Failed to write buffer write to device: %v", err)
	}

	s.TargetDevFd.Sync()

	return nil
}

func (s *Server) CopyJournalToReplica(jrn *journal.Journal) error {
	for i := uint64(0); i < jrn.CorrectOffset; {
		correctBlock, err := jrn.ReadCorrectBlock(i)
		if err != nil {
			return fmt.Errorf("Failed to copy journal to replica: %v", err)
		}

		s.WriteCorrectBlockToReplica(correctBlock)
		i++
	}
	s.TargetDevFd.Sync()

	for i := uint64(0); i < jrn.WriteOffset; {
		correctBlock, err := jrn.ReadBufferWrite(i)
		if err != nil {
			return fmt.Errorf("Failed to copy journal to replica: %v", err)
		}

		s.WriteBufferWriteToReplica(correctBlock)
		i++
	}

	s.VerbosePrintln("Journal copied into replica.")
	jrn.Invalidate()

	return nil
}

func (s *Server) CheckJournal() error {
	jrn, err := journal.OpenJournal(s.Config.JournalPath)
	if err != nil {
		s.DebugPrintln("There is corrupted journal on specified path, continuing...")
		return nil
	}

	if !jrn.IsValid() {
		s.DebugPrintln("Journal on specified path is invalid, continuing...")
		s.DebugPrintln(jrn.String())
		return nil
	}

	s.Println("Journal on specified path is valid, copying the journal to replica...")
	s.VerbosePrintln(jrn.String())

	err = s.CopyJournalToReplica(jrn)
	if err != nil {
		return fmt.Errorf("Failed to copy journal to replica: %v", err)
	}

	return nil
}

// Run starts the server and handles termination signals
func (s *Server) Run() {
	s.Println("Starting bdr server listening on", s.Config.IpAddress, "and port", s.Config.Port)
	s.VerbosePrintln("Target device:", s.Config.TargetDevPath)

	err := s.CheckJournal()
	if err != nil {
		s.Println("Can't create journal:", err)
		return
	}

	var termWg sync.WaitGroup
	termWg.Add(1)
	go s.HandleConnections(&termWg)

	s.StartStatsPrinting(BenchmarkPrintInterval * time.Second)

	// Set up signal handling for graceful shutdown
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	<-signalChan

	s.Println("Interrupt signal received. Shutting down...")
	s.InitiateShutdown() // Signal termination to all goroutines

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
