package networking

import (
	"encoding/gob"
)

const (
	HashedBlockSize = 1 // megabytes
	HashedSpace = 1024 * 1024 * HashedBlockSize
	HashSize = 32 // size of hash in bytes
)

const (
	PacketTypeCmdGetHashes = 0
)

type WriteInfoPacket struct {
	Sector uint32
	Size   uint32
	Data   []byte
}

type HashPacket struct {
	Offset uint32
	Size uint32
	Data [HashSize]byte
}

type Packet struct {
	PacketType uint32
	Payload    any
}

func RegisterGobPackets() {
	gob.Register(WriteInfoPacket{})
	gob.Register(HashPacket{})
}
