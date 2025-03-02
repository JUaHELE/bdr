package networking

import (
	"encoding/gob"
	"fmt"
)

const (
	HashedBlockSize = 1 // megabytes
	HashedSpace = 1024 * 1024 * HashedBlockSize
	HashSize = 32 // size of hash in bytes
)

const (
	PacketTypeCmdGetHashes = iota
	PacketTypeWriteInfo
)

type WriteInfo struct {
	Sector uint32
	Size   uint32
	Data   []byte
}

func (w WriteInfo) Print() {
	fmt.Printf("WriteInfo { Sector: %d, Size: %d, Data:... }\n", w.Sector, w.Size)
}

type HashInfo struct {
	Offset uint32
	Size uint32
	Data [HashSize]byte
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
}
