package networking

import (
	"encoding/gob"
	"fmt"
)

const (
	HashedBlockSize = 1 // 256KB
	HashedSpaceBase = 1024 * 256 * HashedBlockSize
	HashSizeSha256 = 32 // size of hash in bytes
	RepairBlockSize = 1048576 // 1MB
)

const (
	PacketTypeCmdGetHashes = iota
	PacketTypeWriteInfo
	PacketTypeInit
	PacketTypeErrInit
	PacketTypeSha256
	PacketTypeInfoHashingCompleted
	PacketTypeHash
	PacketTypeHashError
	PacketTypeCorrectBlock
	PacketTypeBitmapBlock
)

type CorrectBlockInfo struct {
	Offset uint64
	Size uint32
	Data []byte
}

type InitInfo struct {
	SectorSize uint32
	DeviceSize uint64
}

func (i InitInfo) Print() {
	fmt.Printf("InitInfo { SectorSize: %d, DiskSize: %d }\n", i.SectorSize, i.DeviceSize)
}

type WriteInfo struct {
	Offset uint64
	Size   uint32
	Data   []byte
}

type HashInfo struct {
	Offset uint64
	Size uint32
	Hash uint64
}

func (w WriteInfo) Print() {
	fmt.Printf("WriteInfo { Sector: %d, Size: %d, Data:... }\n", w.Offset, w.Size)
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
	gob.Register(CorrectBlockInfo{})
}
