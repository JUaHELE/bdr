package main

import (
	"bdr/networking"
	"encoding/gob"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"
	"bdr/utils"
)

type atomic_t int32
type BufferStats struct {
	TotalWrites   atomic_t
	OverflowCount atomic_t
	TotalReads    atomic_t
}

type BufferInfo struct {
	Offset    uint64
	Length    uint64
	Last      uint64
	Flags     uint32
	MaxWrites uint64
}

type TargetInfo struct {
	PageSize       uint64
	WriteInfoSize  uint32
	BufferByteSize uint64
}

type Client struct {
	Config       *Config
	TargetInfo   *TargetInfo
	Conn         net.Conn
	Encoder      *gob.Encoder
	Decoder      *gob.Decoder
	UnderDevFile *os.File
	CharDevFile  *os.File // Path to character device which is used for communication with kernel
	Buf          []byte   // Shared buffer between kernel and userspace where writes will be saved
}

var (
	BufferOverflownFlag = utils.Bit(0)
)

func (c *Client) Println(args ...interface{}) {
	c.Config.Println(args...)
}

func (c *Client) VerbosePrintln(args ...interface{}) {
	c.Config.VerbosePrintln(args...)
}

func (c *Client) DebugPrintln(args ...interface{}) {
	c.Config.DebugPrintln(args...)
}

func CheckBufferOverflow(flags uint32) bool {
	return (flags & BufferOverflownFlag) != 0
}

func NewClient(cfg *Config) (*Client, error) {
	// open control device
	charDevFd, err := os.OpenFile(cfg.CharDevicePath, os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("falied to open charecter device: %w", err)
	}

	var targetInfo TargetInfo
	err = ioctl(charDevFd.Fd(), BDR_CMD_GET_TARGET_INFO, uintptr(unsafe.Pointer(&targetInfo)))
	if err != nil {
		charDevFd.Close()
		return nil, fmt.Errorf("ioctl get info from target failed: %v", err)
	}

	// mmap shared buffer
	buf, err := syscall.Mmap(int(charDevFd.Fd()), 0, int(targetInfo.BufferByteSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		charDevFd.Close()
		return nil, fmt.Errorf("mmap failed: %w", err)
	}

	// build ip address of the remote server and attempt to make the connection
	address := fmt.Sprintf("%s:%d", cfg.IpAddress, cfg.Port)
	conn, err := net.Dial("tcp", address)
	if err != nil {
		charDevFd.Close()
		return nil, fmt.Errorf("failed to connect to receiver: %w", err)
	}

	encoder := gob.NewEncoder(conn)
	decoder := gob.NewDecoder(conn)

	// open file for replication
	underDevFd, err := os.OpenFile(cfg.UnderDevicePath, os.O_RDWR, 0600)
	if err != nil {
		charDevFd.Close()
		conn.Close()
		return nil, fmt.Errorf("failed to open target device: %w", err)
	}

	return &Client{
		Config:       cfg,
		Conn:         conn,
		Encoder:      encoder,
		Decoder:      decoder,
		UnderDevFile: underDevFd,
		CharDevFile:  charDevFd,
		Buf:          buf,
	}, nil
}

func (c *Client) Close() {
	if c.Conn != nil {
		c.Conn.Close()
	}
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

func (c *Client) MonitorChanges(termChan <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()
	// reader := bytes.NewReader(nil)

	for {
		select {
		case <-termChan:
			c.VerbosePrintln("Stopping change monitoring due to shutdown.")
			return
		default:
			var bufferInfo BufferInfo
			err := ioctl(c.CharDevFile.Fd(), BDR_CMD_GET_BUFFER_INFO_WAIT, uintptr(unsafe.Pointer(&bufferInfo)))
			if err != nil {
				log.Printf("ioctl syscall failed: %v", err)
			}
		}
	}
}

func (c *Client) Run() {
	termChan := make(chan struct{})
	signalChan := make(chan os.Signal, 1)

	var termWg sync.WaitGroup
	termWg.Add(1) // Indicate we're starting one goroutine
	go c.MonitorChanges(termChan, &termWg)

	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	<-signalChan // Block until signal received

	c.Println("Interrupt signal received. Shutting down...")
	close(termChan) // Signal monitor to stop
	termWg.Wait()   // Wait for monitor to finish
	c.Close()       // Clean up
}

func main() {
	cfg := NewConfig()

	err := ValidateArgs(&cfg.CharDevicePath, &cfg.UnderDevicePath, &cfg.IpAddress, &cfg.Port)
	if err != nil {
		log.Fatalf("Invalid arguments: %v", err)
	}

	sender, err := NewClient(cfg)
	if err != nil {
		log.Fatalf("Failed to initiate sender: %v", err)
	}
	defer sender.Close()

	// register needed packets
	networking.RegisterGobPackets()

}
