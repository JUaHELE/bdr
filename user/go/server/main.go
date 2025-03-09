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

	simdsha256 "github.com/minio/sha256-simd"
)

const (
	PollInterval  = 100 // in milliseconds
	RetryInterval = 1   // seconds
)

const (
	ConnectionTimeout = 30 * time.Second
	ReconnectDelay    = 5 * time.Second
)

const (
	WritePacketQueueSize = 100
)

func RetrySleep() {
	time.Sleep(RetryInterval * time.Second)
}

type Server struct {
	Config      *Config
	Listener    net.Listener
	ClientInfo *networking.InitInfo
	Conn        net.Conn
	Encoder     *gob.Encoder
	Decoder     *gob.Decoder
	TargetDevFd *os.File
	TermChan    chan struct{}
	Connected   bool
	ConnMutex   sync.Mutex
}

func (s *Server) Println(args ...interface{}) {
	s.Config.Println(args...)
}

func (s *Server) VerbosePrintln(args ...interface{}) {
	s.Config.VerbosePrintln(args...)
}

func (s *Server) DebugPrintln(args ...interface{}) {
	s.Config.DebugPrintln(args...)
}


func NewServer(cfg *Config) (*Server, error) {
	targetDeviceFd, err := os.OpenFile(cfg.TargetDevPath, os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open device: %w", err)
	}

	address := fmt.Sprintf("%s:%d", cfg.IpAddress, cfg.Port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		targetDeviceFd.Close()
		return nil, fmt.Errorf("failed to listen on port %d: %w", cfg.Port, err)
	}

	server := &Server{
		Config:      cfg,
		Listener:    listener,
		TargetDevFd: targetDeviceFd,
		TermChan:    make(chan struct{}),
		Connected:   false,
	}

	return server, nil
}

func (s *Server) sendPacket(packet *networking.Packet) {
	for {
		err := s.Encoder.Encode(packet)
		if err != nil {
			if terminated := s.CheckTermination(); terminated {
				s.VerbosePrintln("Terminating attepmt for successfull packet send...")
				return
			}

			s.VerbosePrintln("SendPacket failed: ", err)
			RetrySleep()
			continue
		}
		break
	}
}

func (s *Server) receivePacket(packet *networking.Packet) {
	for {
		err := s.Decoder.Decode(packet)
		if err != nil {
			if terminated := s.CheckTermination(); terminated {
				s.VerbosePrintln("Terminating attepmt for successfull packet send...")
				return
			}

			s.VerbosePrintln("receivePacket failed: ", err)
			RetrySleep()
			continue
		}
		break
	}
}

func (s *Server) CompleteHashing() {
	packet := &networking.Packet{
		PacketType: networking.PacketTypeInfoHashingCompleted,
		Payload: nil,
	}

	err := s.Encoder.Encode(packet)
	if err != nil {
		s.VerbosePrintln("Error while sending complete hashing info:", err)
		return
	}

	s.VerbosePrintln("Hashing completed.")
}

// TODO: parallelize
func (s *Server) hashDiskAndSend(termChan chan struct{}, hashedSpace uint64) {
	s.VerbosePrintln("Hashing disk...")

	buf := make([]byte, hashedSpace)

	readOffset := uint64(0)

	for {
		if terminated := utils.ChanHasTerminated(termChan); terminated {
			s.Println("Hashing has terminated!")
			return
		}

		n, err := s.TargetDevFd.ReadAt(buf, int64(readOffset))
		if err != nil && err != io.EOF {
			s.VerbosePrintln("Error while hashing disk:", err)

			errPacket := networking.Packet{
				PacketType: networking.PacketTypeHashError,
			}
			s.sendPacket(&errPacket)

			return
		}

		if n == 0 {
			break
		}

		shaWriter := simdsha256.New()
		shaWriter.Write(buf)

		hash := shaWriter.Sum(nil)

		hashInfo := networking.HashInfo{
			Offset: readOffset,
			Size: uint32(hashedSpace),
			Hash: hash,
		}

		hashPacket := networking.Packet{
			PacketType: networking.PacketTypeHash,
			Payload: hashInfo,
		}

		if err := s.Encoder.Encode(&hashPacket); err != nil {
			s.VerbosePrintln("Error while sending complete hashing info:", err)

			errPacket := networking.Packet{
				PacketType: networking.PacketTypeHashError,
			}
			s.sendPacket(&errPacket)
			return
		}

		readOffset += uint64(hashedSpace)
	}

	s.CompleteHashing()
}

func (s *Server) CheckTermination() bool {
	select {
	case <-s.TermChan:
		return true
	default:
		return false
	}
}

func (s *Server) Close() {
	s.ConnMutex.Lock()
	defer s.ConnMutex.Unlock()

	if s.Conn != nil {
		s.Conn.Close()
		s.Conn = nil
	}
	if s.Listener != nil {
		s.Listener.Close()
	}
	if s.TargetDevFd != nil {
		s.TargetDevFd.Close()
	}
}

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

func (s *Server) handleWriteInfoPacket(writeChan chan *networking.Packet) {
	for packet := range writeChan {
		s.DebugPrintln("Write infomation packet received.")
		writeInfo, ok := packet.Payload.(networking.WriteInfo)
		if !ok {
			s.VerbosePrintln("invalid payload type for WriteInfo")
		}

		writeOffset := int64(writeInfo.Sector) * int64(s.ClientInfo.SectorSize)
		dataToWrite := writeInfo.Data[:writeInfo.Size]

		if _, err := s.TargetDevFd.WriteAt(dataToWrite, writeOffset); err != nil {
			s.VerbosePrintln("Failed to write data to device:", err)
		}
	}
}

func (s *Server) WaitForInitInfo() error {
	packet := &networking.Packet{}
	if err := s.Decoder.Decode(packet); err != nil {
		if err == io.EOF {
			return fmt.Errorf("Connection closed by the client.")
		}
		return fmt.Errorf("Failed to decode packet: %v", err)
	}

	if packet.PacketType != networking.PacketTypeInit {
		return fmt.Errorf("Expected init packet, got: %d", packet.PacketType)
	}

	initInfo, ok := packet.Payload.(networking.InitInfo)
	if !ok {
		return fmt.Errorf("invalid payload type for init packet")
	}

	s.ClientInfo = &initInfo
	s.VerbosePrintln("Init info received...")

	return nil
}

func (s *Server) CheckValidSizes() bool {
	deviceSize, err := utils.GetDeviceSize(s.Config.TargetDevPath)
	if err != nil || deviceSize != s.ClientInfo.DeviceSize{
		errPacket := networking.Packet{
			PacketType: networking.PacketTypeErrInit,
			Payload: nil,
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

func (s *Server) handleCorrectPacket(packet *networking.Packet) {
	s.DebugPrintln("Writing correct block...")

	correctInfo, ok := packet.Payload.(networking.CorrectBlockInfo)
	if !ok {
		s.VerbosePrintln("invalid packet type for correctblock")
	}
	
	if _, err := s.TargetDevFd.WriteAt(correctInfo.Data, int64(correctInfo.Offset)); err != nil {
		s.VerbosePrintln("Can't write correct block")
		// TODO: solve this - maybe ask for it again
	}
}

func (s *Server) HandleClient(wg *sync.WaitGroup) {
	defer s.CloseClientConn()
	defer wg.Done()

	s.Println("Accepted connection from", s.Conn.RemoteAddr())

	hashingTermChan := make(chan struct{})

	writeQueue := make(chan *networking.Packet, WritePacketQueueSize)
	defer close(writeQueue)

	go s.handleWriteInfoPacket(writeQueue)

	if err := s.WaitForInitInfo(); err != nil {
		s.Println("Error occured while waiting on init packet:", err)
		return
	}

	if valid := s.CheckValidSizes(); !valid {
		return
	}

	for {
		if s.CheckTermination() {
			s.VerbosePrintln("Terminating client handler.")
			return
		}

		packet := &networking.Packet{}
		if err := s.Decoder.Decode(packet); err != nil {
			if err == io.EOF {
				s.Println("Connection closed by the client.")
				return
			}
			s.VerbosePrintln("Failed to decode packet:", err)
			continue
		}

		switch packet.PacketType {
		case networking.PacketTypeCmdGetHashes:
			close(hashingTermChan)
			hashingTermChan = make(chan struct{})
			go s.hashDiskAndSend(hashingTermChan, networking.HashedSpaceBase)
		case networking.PacketTypeWriteInfo:
			writeQueue <- packet
		case networking.PacketTypeCorrectBlock:
			s.DebugPrintln("Correct block arrived")
			go s.handleCorrectPacket(packet)
		default:
			s.VerbosePrintln("Unknown packet received:", packet.PacketType)
		}
	}
}

func (s *Server) HandleConnections(wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		if s.CheckTermination() {
			s.VerbosePrintln("Terminating connection listener.")
			return
		}

		conn, err := s.Listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				s.VerbosePrintln("Listener closed. Terminating...")
				break
			}

			s.Println("Listener.Accept error:", err)
			continue
		}

		s.ConnMutex.Lock()
		if s.Connected {
			conn.Close()
			s.ConnMutex.Unlock()
			time.Sleep(ReconnectDelay)
			continue
		}

		s.Conn = conn
		s.Encoder = gob.NewEncoder(conn)
		s.Decoder = gob.NewDecoder(conn)
		s.Connected = true

		s.ConnMutex.Unlock()

		var clientWg sync.WaitGroup
		clientWg.Add(1)

		go s.HandleClient(&clientWg)
		clientWg.Wait()

		s.VerbosePrintln("Client disconnected.")
	}
}

func (s *Server) Run() {
	s.Println("Starting bdr server listening on", s.Config.IpAddress, "and port", s.Config.Port)
	s.VerbosePrintln("Target device:", s.Config.TargetDevPath)

	var termWg sync.WaitGroup
	termWg.Add(1)
	go s.HandleConnections(&termWg)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	<-signalChan

	s.Println("Interrupt signal received. Shutting down...")
	close(s.TermChan) // Use the servers's TermChan

	s.Listener.Close()
	termWg.Wait()
	s.Close()

	s.Println("bdr server terminated successfully.")
}

func main() {
	cfg := NewConfig()

	err := ValidateArgs(&cfg.TargetDevPath, &cfg.Port, &cfg.IpAddress)
	if err != nil {
		log.Fatalf("Invalid arguments: %v", err)
	}

	server, err := NewServer(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}
	defer server.Close()

	networking.RegisterGobPackets()

	server.Run()
}
