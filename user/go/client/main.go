package main

import (
	"fmt"
	"log"
	"os"
	"syscall"
	"unsafe"
)

type atomic_t int32
type BufferStats struct {
	TotalWrites atomic_t;
	OverflowCount atomic_t;
	TotalReads atomic_t;
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

const (
	devicePath = "/dev/bdr-1"
	bufferSize = 1024
)

func main() {
	// Open the character device
	fd, err := os.OpenFile(devicePath, os.O_RDWR, 0)
	if err != nil {
		log.Fatalf("Failed to open device: %v", err)
	}
	defer fd.Close()

	// Memory map the buffer
	mmapData, err := syscall.Mmap(
		int(fd.Fd()),
		0,
		bufferSize,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_SHARED,
	)
	if err != nil {
		log.Fatalf("Failed to mmap: %v", err)
	}
	defer syscall.Munmap(mmapData)

	var targetInfo BufferInfo
	err = ioctl(fd.Fd(), BDR_CMD_GET_BUFFER_INFO, uintptr(unsafe.Pointer(&targetInfo)))
	if err != nil {
		fmt.Printf("Failed to get info: %v\n", err)
	} else {
		fmt.Printf("Device info: %d, %d, %d, %d, %d\n", targetInfo.Offset, targetInfo.Length, targetInfo.Last, targetInfo.Flags, targetInfo.MaxWrites)
	}
}
