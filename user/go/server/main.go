package main

import (
	"bdr"
	"crypto/sha256"
	"encoding/gob"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	DefaultDevicePath = ""
	DefaultIpAddress  = ""
	DefaultPort       = 0
	DefaultVerbose    = false
)

// Config holds data passed by arguments
type Config struct {
	DevicePath string // Of device which is to be served as a replica
	IpAddress  string
	Port       int
	Verbose    bool
}

// Receiver encaptulates the receiver's state and configuration
type Receiver struct {
	Config        *Config
	ListenAddr    string
	Conn          net.Conn
	Encoder       *gob.Encoder
	Decoder       *gob.Decoder
	ReadDeviceFd  *os.File
	WriteDeviceFd *os.File
}

// Creation of new config
func NewConfig() *Config {
	cfg := &Config{}

	// WARNING
	// - the devices have to be atleast the same size for the program to properly work
	// - once the connection is established, the device will become a copy of remote disk (all data will be removed)
	flag.StringVar(&cfg.DevicePath, "d", DefaultDevicePath, "Path to the block device which is meant as a replica")
	flag.StringVar(&cfg.IpAddress, "I", DefaultIpAddress, "Sender IP address")
	flag.IntVar(&cfg.Port, "p", DefaultPort, "Sender port")
	flag.BoolVar(&cfg.Verbose, "v", DefaultVerbose, "Provides verbose output of the program")
	flag.Parse()
	return cfg
}

// initialization of new Receiver instance
func NewReceiver(cfg *Config) (*Receiver, error) {
	readDeviceFd, err := os.OpenFile(cfg.DevicePath, os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open device: %w", err)
	}

	writeDeviceFd, err := os.OpenFile(cfg.DevicePath, os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open device: %w", err)
	}

	return &Receiver{
		Config:        cfg,
		ReadDeviceFd:  readDeviceFd,
		WriteDeviceFd: writeDeviceFd,
		ListenAddr:    fmt.Sprintf("%s:%d", cfg.IpAddress, cfg.Port),
	}, nil
}

// cleans Receiver resources
func (r *Receiver) Close() {
	if r.Conn != nil {
		r.Conn.Close()
	}
	if r.ReadDeviceFd != nil {
		r.ReadDeviceFd.Close()
	}
	if r.WriteDeviceFd != nil {
		r.WriteDeviceFd.Close()
	}
}

func chanWithCancel() (chan struct{}, func()) {
	ch := make(chan struct{})
	cancel := func() { close(ch) }
	return ch, cancel
}

func (r *Receiver) startServer(ch <-chan struct{}) {
	listener, err := net.Listen("tcp", r.ListenAddr)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	defer listener.Close()
	log.Printf("Receiver listening on %s", r.ListenAddr)

	for {
		select {
		case <-ch:
			log.Println("Shutting down server.")
			return
		default:
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Failed to accept connection: %v", err)
				continue
			}
			log.Printf("Accepted connection from %s", conn.RemoteAddr())
			go r.handleClient(conn, ch)
		}
	}
}

func getWriteOffset(bioInfo *bdr.UserBioInfo) uint32 {
	return bioInfo.Sector * bdr.SectorSize
}

// writes regular write block with size of single page
func (r *Receiver) writeBlock(packet *bdr.Packet) error {
	bioInfo, ok := packet.Payload.(bdr.UserBioInfo)
	if !ok {
		return fmt.Errorf("invalid payload type for BDR_PACKET_WRITE")
	}

	writeOffset := getWriteOffset(&bioInfo)
	dataToWrite := bioInfo.Data[:bioInfo.Size]

	if _, err := r.WriteDeviceFd.Seek(int64(writeOffset), io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek in device: %w", err)
	}

	if _, err := r.WriteDeviceFd.Write(dataToWrite); err != nil {
		return fmt.Errorf("failed to write data to device: %w", err)
	}

	return nil
}

func (r *Receiver) Printf(message string) {
	if r.Config.Verbose {
		log.Printf(message)
	}
}

// send completion packet until we ensure that it is sent, or the monitoring won't start
func (r *Receiver) CompleteHashing() {
	packet := bdr.Packet{
		PacketType: bdr.PacketHashingCompleted,
		Payload:    nil,
	}

	err := r.Encoder.Encode(packet)

	for err != nil {
		err = r.Encoder.Encode(packet)
		time.Sleep(1 * time.Second)
	}

	r.Printf("Complete hashing packet sent.")
}

// computes hashes of the target device's data and sends them to the sender.
func (r *Receiver) hashDeviceAndSend() error {
	// hashes blocks of size HashedSpace and sends them to sender, where the comparison happens
	r.Printf("Hashing disk blocks and sending then over to the sender daemon...")
	buf := make([]byte, bdr.HashedSpace)
	r.ReadDeviceFd.Seek(0, io.SeekStart)

	offset := uint32(0)

	for {
		n, err := r.ReadDeviceFd.Read(buf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("error reading device: %w", err)
		}
		if n == 0 {
			break
		}

		// Compute SHA-256 hash
		hash := sha256.Sum256(buf[:n])

		packetPayload := bdr.HashPacket{
			Offset: offset,
			Size:   uint32(n),
			Data:   hash,
		}

		// Create and send hash packet
		packet := bdr.Packet{
			PacketType: bdr.PacketHash,
			Payload:    packetPayload,
		}

		// here and before whenever hashing breaks, we need to have some mechanism to try it again
		if err := r.Encoder.Encode(packet); err != nil {
			return fmt.Errorf("error sending the packet %w", err)
		}

		offset += 1
	}

	// send completion packet
	r.CompleteHashing()

	r.Printf("Device successfully hashed and sent over the network.")
	return nil
}

// write space that need to be replicated
func (r *Receiver) WriteBigOne(packet *bdr.Packet) error {
	r.Printf("Writing big one.")
	bigInfo, ok := packet.Payload.(bdr.DifferentPiecePacket)
	if !ok {
		return fmt.Errorf("invalid payload type for PacketDiffrentPiece")
	}

	writeOffset := bigInfo.Offset * bdr.HashedSpace
	dataToWrite := bigInfo.Data[:bigInfo.Size]

	if _, err := r.WriteDeviceFd.Seek(int64(writeOffset), io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek in device: %w", err)
	}

	if _, err := r.WriteDeviceFd.Write(dataToWrite); err != nil {
		return fmt.Errorf("failed to write data to device: %w", err)
	}

	return nil
}

func (r *Receiver) handleConnection(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
			r.Printf("Stopping connection handler")
			return
		default:
			packet := &bdr.Packet{}
			if err := r.Decoder.Decode(packet); err != nil {
				if err == io.EOF {
					log.Println("Connection closed by sender.")
					return
				}
				log.Printf("Failed to decode packet: %v", err)
				continue
			}

			switch packet.PacketType {
			case bdr.PacketWrite:
				// packet write should not be received when replication happens
				if err := r.writeBlock(packet); err != nil {
					log.Printf("Failed to replicate default data: %v", err)
				}
			case bdr.PacketSendHashes:
				// this will be executed in parallel in future versions
				if err := r.hashDeviceAndSend(); err != nil {
					log.Println("Failed to hash the device: %v", err)
				}
			case bdr.PacketDifferentPiece:
				r.Printf("Big packet arrived.")
				if err := r.WriteBigOne(packet); err != nil {
					log.Println("Failed to write bigone: %v", err)
				}
			default:
				log.Printf("Unknown packet type received: %d", packet.PacketType)
			}
		}
	}
}

// checks if necessary arguments are not missing
func ValidateArgs(devicePath *string, ipAddress *string, port *int) error {
	var missingArgs []string

	// Nil checks to avoid dereferencing nil pointers
	if devicePath == nil || *devicePath == "" {
		missingArgs = append(missingArgs, "-d (device path)")
	}
	if ipAddress == nil || *ipAddress == "" {
		missingArgs = append(missingArgs, "-I (IP address)")
	}
	if port == nil || *port == 0 {
		missingArgs = append(missingArgs, "-p (port)")
	}

	// If there are missing arguments, return an error
	if len(missingArgs) > 0 {
		return fmt.Errorf("missing required arguments: %s", strings.Join(missingArgs, ", "))
	}

	return nil
}

func (r *Receiver) handleClient(conn net.Conn, ch <-chan struct{}) {
	defer conn.Close()

	r.Conn = conn
	r.Encoder = gob.NewEncoder(conn)
	r.Decoder = gob.NewDecoder(conn)

	r.handleConnection(ch)
}

func (r *Receiver) Run() {
	ch, cancel := chanWithCancel()

	go r.startServer(ch)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	<-signalChan
	r.Printf("Interrupt signal received. Shutting down...")
	cancel()
	log.Println("Receiver daemon terminated.")
}

func main() {
	// Initilize configuration
	cfg := NewConfig()

	if err := ValidateArgs(&cfg.DevicePath, &cfg.IpAddress, &cfg.Port); err != nil {
		log.Fatalf("Invalid arguments: %v", err)
	}

	receiver, err := NewReceiver(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize receiver: %v", err)
	}
	defer receiver.Close()

	// register needed packets
	bdr.RegisterGobPackets()

	receiver.Run()
}
