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
	Config            *Config
	TargetInfo        *TargetInfo
	Conn              net.Conn
	Encoder           *gob.Encoder
	Decoder           *gob.Decoder
	UnderDevFile      *os.File
	CharDevFile       *os.File               // Path to character device which is used for communication with kernel
	Buf               []byte                 // Shared buffer between kernel and userspace where writes will be saved
	MonitorPauseContr *pause.PauseController // used to pause monitor changes function
	TermChan          chan struct{}          // Add termination channel
}

var (
	BufferOverflownFlag = utils.Bit(0)
)

const (
	PollInterval  = 100 // in milliseconds
	RetryInterval = 1   // seconds
)

func RetrySleep() {
	time.Sleep(RetryInterval * time.Second)
}

func (c *Client) Println(args ...interface{}) {
	c.Config.Println(args...)
}

func (c *Client) VerbosePrintln(args ...interface{}) {
	c.Config.VerbosePrintln(args...)
}

func (c *Client) DebugPrintln(args ...interface{}) {
	c.Config.DebugPrintln(args...)
}

func (b BufferStats) Print() {
	fmt.Printf("BufferStats { TotalWrites: %d, OverflowCount: %d, TotalReads: %d }\n", b.TotalWrites, b.OverflowCount, b.TotalReads)
}

func (b BufferInfo) Print() {
	fmt.Printf("BufferInfo { Offset: %d, Length: %d, Last: %d, Flags: %d, MaxWrites: %d }\n", b.Offset, b.Length, b.Last, b.Flags, b.MaxWrites)
}

func (t TargetInfo) Print() {
	fmt.Printf("TargetInfo { PageSize: %d, WriteInfoSize: %d, BufferByteSize: %d }\n", t.PageSize, t.WriteInfoSize, t.BufferByteSize)
}

func (b BufferInfo) CheckOverflow() bool {
	return (b.Flags & BufferOverflownFlag) != 0
}

func (b BufferInfo) HasNewWrites() bool {
	return b.Length != 0
}

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

func (c *Client) ResetBufferIOCTL() {
	c.VerbosePrintln("Reseting buffer...")
	c.serveIOCTL(BDR_CMD_RESET_BUFFER, 0)
}

func (c *Client) GetBufferInfoByteOffset(bufferInfo *BufferInfo, off uint64) uint64 {
	offset := ((bufferInfo.Offset + off) % bufferInfo.MaxWrites) * uint64(c.TargetInfo.WriteInfoSize)
	return offset
}

func (c *Client) executeIOCTL(cmd uintptr, arg uintptr) error {
	err := ioctl(c.CharDevFile.Fd(), cmd, arg)
	if err != nil {
		cmdName := GetIOCTLCommandName(cmd)
		return fmt.Errorf("%s (0x%X) failed: %v", cmdName, cmd, err)
	}
	return nil
}

// wrapper around ioctl calls, tries indefinitely until the problem is fixed
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

func (c *Client) reconnectToServer() {
	c.VerbosePrintln("Trying to reconnect to the server.")

	if c.Conn != nil {
		c.Conn.Close()
	}

	address := fmt.Sprintf("%s:%d", c.Config.IpAddress, c.Config.Port)

	for {
		if terminated := c.CheckTermination(); terminated {
			c.VerbosePrintln("Terminating attepmt for reconnect...")
			return
		}

		conn, err := net.DialTimeout("tcp", address, 10*time.Second)
		if err == nil {
			c.Conn = conn
			c.Encoder = gob.NewEncoder(conn)
			c.Decoder = gob.NewDecoder(conn)

			// Resend initialization packet
			if err := c.SendInitPacket(c.Config.UnderDevicePath); err != nil {
				c.Println("Failed to resend init packet during reconnection")
				continue
			}

			c.Println("Successfully reconnected to server")
			return
		}

		c.VerbosePrintln("Attempt for reconnection failed. Trying again...")
	}
}

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

func (c *Client) receivePacket(packet *networking.Packet) {
	for {
		err := c.Decoder.Decode(packet)
		if err != nil {
			if terminated := c.CheckTermination(); terminated {
				c.VerbosePrintln("Terminating attepmt for successfull packet send...")
				return
			}

			c.VerbosePrintln("receivePacket failed: ", err)
			RetrySleep()
			continue
		}
		break
	}
}

func (c *Client) SendInitPacket(device string) error {
	sectorSize, err := utils.GetSectorSize(device);
	if err != nil {
		return err
	}

	deviceSize, err := utils.GetDeviceSize(device);
	if err != nil {
		return err
	}

	initPacket := networking.InitInfo{
		SectorSize: sectorSize,
		DeviceSize: deviceSize,
	}

	packet := networking.Packet{
		PacketType: networking.PacketTypeInit,
		Payload: initPacket,
	}

	c.sendPacket(&packet)

	return nil
}

func (c *Client) sendCorrectBlock(buf []byte, offset uint64, size uint32) {
	correctBlockInfo := &networking.CorrectBlockInfo{
		Offset: offset,
		Size: size,
		Data: buf,
	}

	correctBlockPacket := &networking.Packet{
		PacketType: networking.PacketTypeCorrectBlock,
		Payload: correctBlockInfo,
	}

	c.sendPacket(correctBlockPacket)
}

func (c *Client) handleHashPacket(packet *networking.Packet, wg *sync.WaitGroup) {
	defer wg.Done()

	hashInfo, ok := packet.Payload.(networking.HashInfo)
	if !ok {
		c.VerbosePrintln("invalid payload for type for HashInfo")
		c.InitiateCheckedReplication()
		return
	}

	buf := make([]byte, hashInfo.Size)
	if _, err := c.UnderDevFile.ReadAt(buf, int64(hashInfo.Offset)); err != nil && err != io.EOF {
		c.VerbosePrintln("Error while hashing...")
		c.InitiateCheckedReplication()
		return
	}

	shaWriter := simdsha256.New()
	shaWriter.Write(buf)

	hash := shaWriter.Sum(nil)

	if !bytes.Equal(hash[:], hashInfo.Hash[:]) {
		c.DebugPrintln("Blocks are not equal")
		c.sendCorrectBlock(buf, hashInfo.Offset, hashInfo.Size)
		return
	}

	c.DebugPrintln("Blocks are equal...")
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


	monitorPauseContr := pause.NewPauseController()

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

func (c *Client) CloseClientConn() {
	if c.Conn != nil {
		c.Conn.Close()
	}
}

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

func (c *Client) InitiateCheckedReplication() {
	// reset the buffer since the data will still be replicated
	c.ResetBufferIOCTL()

	c.MonitorPauseContr.Pause()

	packet := networking.Packet{
		PacketType: networking.PacketTypeCmdGetHashes,
		Payload:    nil,
	}

	c.sendPacket(&packet)
}

func (c *Client) ProcessBufferInfo(bufferInfo *BufferInfo) {
	for i := uint64(0); i < bufferInfo.Length; i++ {
		// if monitor is paused, we are hashing and there is no point of continuing
		if c.MonitorPauseContr.IsPaused() {
			return
		}

		for {
			bufBegin := c.GetBufferInfoByteOffset(bufferInfo, i)
			bufEnd := bufBegin + uint64(c.TargetInfo.WriteInfoSize)
			data := c.Buf[bufBegin:bufEnd]

			reader := bytes.NewReader(data)

			var writeInfoPacket networking.WriteInfo
			var err error

			// Read Sector
			if err = binary.Read(reader, binary.LittleEndian, &writeInfoPacket.Sector); err != nil {
				if terminated := c.CheckTermination(); terminated {
					c.VerbosePrintln("Terminating attempt to process buffer info...")
					return
				}
				c.VerbosePrintln("Failed to read Sector: ", err)
				RetrySleep()
				continue
			}

			// Read Size
			if err = binary.Read(reader, binary.LittleEndian, &writeInfoPacket.Size); err != nil {
				if terminated := c.CheckTermination(); terminated {
					c.VerbosePrintln("Terminating attempt to process buffer info...")
					return
				}
				c.VerbosePrintln("Failed to read Size: ", err)
				RetrySleep()
				continue
			}

			// Read Data
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

			// Now send the packet through the network
			packet := networking.Packet{
				PacketType: networking.PacketTypeWriteInfo,
				Payload:    writeInfoPacket,
			}

			c.sendPacket(&packet)

			// If we got here, we successfully processed this write info
			break
		}
	}
}

func (c *Client) MonitorChanges(wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		// check if there was a termination signal or pause signal sent
		if terminated := c.MonitorPauseContr.WaitIfPaused(c.TermChan); terminated {
			c.VerbosePrintln("Stopping change monitoring due to shutdown.")
			return
		}

		// using ioctl to get information from my character device assotiated to my device mapper target
		var bufferInfo BufferInfo
		c.serveIOCTL(BDR_CMD_READ_BUFFER_INFO, uintptr(unsafe.Pointer(&bufferInfo)))

		// check if new write infomation can be found within the buffer, if not we'll wait and try again
		newWrites := bufferInfo.HasNewWrites()
		if !newWrites {
			c.DebugPrintln("No information available, waiting...")
			time.Sleep(PollInterval * time.Millisecond)
			continue
		}

		// check if buffer overflown, the devices are now inconsistent
		overflow := bufferInfo.CheckOverflow()
		if overflow {
			c.VerbosePrintln("Buffer overflown...")

			// we need to check what parts of the disk are inconsistent, first we need to make sure there is no work being done on the disk, because while we scan the buffer might oveflow again
			for {
				if terminated := c.MonitorPauseContr.WaitIfPaused(c.TermChan); terminated {
					c.VerbosePrintln("Stopping change monitoring due to shutdown.")
					return
				}

				c.serveIOCTL(BDR_CMD_READ_BUFFER_INFO, uintptr(unsafe.Pointer(&bufferInfo)))

				newWrites = bufferInfo.HasNewWrites()
				if newWrites {
					// if new writes are present, there is no point starting replication of the disk, since
					RetrySleep()
					continue
				}

				break
			}

			c.InitiateCheckedReplication()

			continue
		}

		// bufferInfo.Print()
		c.ProcessBufferInfo(&bufferInfo)
	}
}

func (c *Client) handleHashing(packet *networking.Packet) {
	c.DebugPrintln("Starting hashing phase...")
	var hashWg sync.WaitGroup
	defer c.MonitorPauseContr.Resume()
	defer hashWg.Wait()

	hashWg.Add(1)
	go c.handleHashPacket(packet, &hashWg)

	for {
		packet := &networking.Packet{}
		c.receivePacket(packet)

		if c.CheckTermination() {
			c.VerbosePrintln("Terminating hashing handler.")
			return
		}

		switch packet.PacketType {
		case networking.PacketTypeErrInit:
			c.Println("ERROR: Remote and local devices do not have the same size.")
		case networking.PacketTypeHash:
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

func (c *Client) ListenPackets(wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		packet := &networking.Packet{}
		c.receivePacket(packet)

		if c.CheckTermination() {
			c.VerbosePrintln("Terminating packet listener.")
			return
		}

		switch packet.PacketType {
		case networking.PacketTypeErrInit:
			c.Println("ERROR: Remote and local devices do not have the same size.")
		case networking.PacketTypeHash:
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

func (c *Client) Run() {
	c.Println("Starting bdr client connected to", c.Config.IpAddress, "and port", c.Config.Port)

	var termWg sync.WaitGroup
	termWg.Add(1)
	go c.MonitorChanges(&termWg)

	termWg.Add(1)
	go c.ListenPackets(&termWg)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	<-signalChan

	c.Println("Interrupt signal received. Shutting down...")
	close(c.TermChan)
	c.CloseClientConn()
	termWg.Wait()

	c.CloseResources()
	c.Println("bdr client daemon terminated successfully.")
}

func main() {
	cfg := NewConfig()

	err := ValidateArgs(&cfg.CharDevicePath, &cfg.UnderDevicePath, &cfg.IpAddress, &cfg.Port)
	if err != nil {
		log.Fatalf("Invalid arguments: %v", err)
	}

	// register needed packets
	networking.RegisterGobPackets()

	client, err := NewClient(cfg)
	if err != nil {
		log.Fatalf("Failed to initiate sender: %v", err)
	}

	if err := client.SendInitPacket(cfg.UnderDevicePath); err != nil {
		log.Fatalf("failed to sent init packet: %v", err)
	}

	client.Run()
}
