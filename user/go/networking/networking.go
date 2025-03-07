package networking

import (
	"encoding/gob"
	"fmt"
)

const (
	HashedBlockSize = 1 // megabytes
	HashedSpaceBase = 1024 * 1024 * HashedBlockSize
	HashSizeSha256 = 32 // size of hash in bytes
)

const (
	PacketTypeCmdGetHashes = iota
	PacketTypeWriteInfo
	PacketTypeInit
	PacketTypeErrInit
	PacketTypeSha256
	PacketTypeInfoHashingCompleted
)

type InitInfo struct {
	SectorSize uint32
	DeviceSize uint64
}

func (i InitInfo) Print() {
	fmt.Printf("InitInfo { SectorSize: %d, DiskSize: %d }\n", i.SectorSize, i.DeviceSize)
}

type WriteInfo struct {
	Sector uint32
	Size   uint32
	Data   []byte
}

type HashInfo struct {
	Offset uint64
	Size uint32
	Hash []byte
}

func (w WriteInfo) Print() {
	fmt.Printf("WriteInfo { Sector: %d, Size: %d, Data:... }\n", w.Sector, w.Size)
}

func (h HashInfo) Print() {
	fmt.Printf("HashInfo { Offset: %d, Size: %d, Data:... }\n", h.Offset, h.Size)
}

type Packet struct {
	PacketType uint32
	Payload    any
}

func RegisterGobPackets() {
	gob.Register(WriteInfo{})
	gob.Register(HashInfo{})
	gob.Register(InitInfo{})
}
