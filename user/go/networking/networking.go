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
	WriteInfoSize = 4108
)

const (
	PacketTypeCmdStartHashing = iota
	PacketTypeInfoHashingCompleted
	PacketTypeInfoStartHashing
	PacketTypeInfoInit
	PacketTypeErrInit
	PacketTypeErrInvalidSizes
	PacketTypeErrHash
	PacketTypeErrJournalCreate
	PacketTypeErrJournalOverflow
	PacketTypePayloadBufferWrite
	PacketTypePayloadHash
	PacketTypePayloadCorrectBlock
)

type CorrectBlockInfo struct {
	Offset uint64
	Size uint32
	Data []byte
}

func (c *CorrectBlockInfo) IsValid() bool {
	return c.Size != 0
}

var (
	CorrectBlockByteSize = 8 + 4 + HashedSpaceBase // offset + size + hashed space
)

type InitInfo struct {
	DeviceSize uint64
	WriteInfoSize  uint32
	BufferByteSize uint64
}

func (i InitInfo) Print() {
	fmt.Printf("InitInfo { DiskSize: %d, WriteInfoSize: %d, BufferByteSize: %d }\n", i.DeviceSize, i.WriteInfoSize, i.BufferByteSize)
}

type WriteInfo struct {
	Offset uint64
	Size   uint32
	Data   []byte
}

func (w *WriteInfo) IsValid() bool {
	return w.Size != 0
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
