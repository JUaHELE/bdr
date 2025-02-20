package networking

import (
	"encoding/gob"
)

type WriteInfo struct {
	Sector uint32
	Size   uint32
	Data   []byte
}

type Packet struct {
	PacketType uint32
	Payload    any
}

func RegisterGobPackets() {
	gob.Register(WriteInfo{})
}
