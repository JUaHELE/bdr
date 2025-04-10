package journal

import (
	"encoding/binary"
	"errors"
	"os"
	"unsafe"
)

const (
	BdrMagic = 0x4244525F52504C43
)

const (
	JournalValidFlag = iota
)

var (
	ErrInvalidMagic = errors.New("invalid magic bytes in journal header")
	ErrInvalidSize  = errors.New("invalid section size")
	ErrIOFailure    = errors.New("I/O operation failed")
)

type Header struct {
	magic uint64

	bufWriteByteSize uint64
	bufWritesCount   uint64
	bufWritesStartOffset  uint64

	corrBlockByteSize uint64
	corrBlocksCount   uint64
	corrBlocksStartOffset  uint64

	flags uint64
}

func (h *Header) GetBufWriteSectionByteSize() uint64 {
	return h.bufWriteByteSize * h.bufWritesCount
}

func (h *Header) GetCorrBlockSectionByteSize() uint64 {
	return h.corrBlockByteSize * h.corrBlocksCount
}

func (h *Header) SetValidFlag() {
	
}

func (h *Header) IsValid() {
	
}

func GetHeaderByteSize() uint64 {
	var h Header
	return uint64(unsafe.Sizeof(h))
}

func VerifyMagic(header uint64) bool {
	return uint64(BdrMagic) == header
}

type Journal struct {
	disk     *os.File
	diskSize uint64

	header *Header
}

func WriteHeader(disk *os.File, header *Header) error {
	headerBytes := make([]byte, GetHeaderByteSize())

	// Write the magic number
	binary.BigEndian.PutUint64(headerBytes[0:8], header.magic)

	// Write the buffer write section information
	binary.BigEndian.PutUint64(headerBytes[8:16], header.bufWriteByteSize)
	binary.BigEndian.PutUint64(headerBytes[16:24], header.bufWritesCount)
	binary.BigEndian.PutUint64(headerBytes[24:32], header.bufWritesStartOffset)

	// Write the correlation block section information
	binary.BigEndian.PutUint64(headerBytes[32:40], header.corrBlockByteSize)
	binary.BigEndian.PutUint64(headerBytes[40:48], header.corrBlocksCount)
	binary.BigEndian.PutUint64(headerBytes[48:56], header.corrBlocksStartOffset)

	// Write the flags
	binary.BigEndian.PutUint64(headerBytes[56:64], header.flags)

	_, err := disk.WriteAt(headerBytes, 0)
	if err != nil {
		return ErrIOFailure
	}

	err = disk.Sync()
	if err != nil {
		return ErrIOFailure
	}

	return nil
}

func ReadHeader(disk *os.File) (*Header, error) {
	headerSize := GetHeaderByteSize()
	headerBytes := make([]byte, headerSize)

	_, err := disk.ReadAt(headerBytes, 0)
	if err != nil {
		return nil, ErrIOFailure
	}

	
	header := &Header{}
	header.magic = binary.BigEndian.Uint64(headerBytes[0:8])
	header.bufWriteByteSize = binary.BigEndian.Uint64(headerBytes[8:16])
	header.bufWritesCount = binary.BigEndian.Uint64(headerBytes[16:24])
	header.bufWritesStartOffset = binary.BigEndian.Uint64(headerBytes[24:32])

	header.corrBlockByteSize = binary.BigEndian.Uint64(headerBytes[32:40])
	header.corrBlocksCount = binary.BigEndian.Uint64(headerBytes[40:48])
	header.corrBlocksStartOffset = binary.BigEndian.Uint64(headerBytes[48:56])
	header.flags = binary.BigEndian.Uint64(headerBytes[56:64])

	return header, nil
}

func ValidateJournal(journal *Journal, ) {
	if !VerifyMagic(journal.header.magic) {
		return nil, ErrInvalidMagic
	}

	expectedSize := journal.header.bufWritesOffset + journal.header.GetBufWriteSectionByteSize() + journal.header.GetCorrBlockSectionByteSize()
	if journal.diskSize < expectedSize {
		disk.Close()
		return nil, ErrInvalidSize
	}
}

// This function can be used to open an existing journal
func OpenJournal(diskPath string) (*Journal, error) {
	disk, err := os.OpenFile(diskPath, os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	header, err := ReadHeader(disk)
	if err != nil {
		return nil, err
	}

	// Get disk size
	diskInfo, err := disk.Stat()
	if err != nil {
		disk.Close()
		return nil, err
	}
	diskSize := uint64(diskInfo.Size())

	// Validate the disk size against header information

	journal := &Journal{
		disk:     disk,
		diskSize: diskSize,
		header:   header,
	}

	return journal, nil
}

func NewJournal(diskPath string, sectionBufWritesSize uint64, bufWriteByteSize uint64, corrBlockByteSize uint64) (*Journal, error) {
	disk, err := os.OpenFile(diskPath, os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	diskInfo, err := disk.Stat()
	if err != nil {
		disk.Close()
		return nil, err
	}
	diskSize := uint64(diskInfo.Size())

	headerSize := GetHeaderByteSize()
	if sectionBufWritesSize > diskSize-headerSize || sectionBufWritesSize == 0 {
		disk.Close()
		return nil, ErrInvalidSize
	}

	sectionCorBlocksSize := diskSize - headerSize - sectionBufWritesSize

	header := &Header{
		magic: BdrMagic,

		bufWriteByteSize: bufWriteByteSize,
		bufWritesCount:   sectionBufWritesSize / bufWriteByteSize,
		bufWriteStartOffset:  headerSize,

		corrBlockByteSize: corrBlockByteSize,
		corrBlocksCount:   sectionCorBlocksSize / corrBlockByteSize,
		corrBlocksStartOffset:  headerSize + sectionBufWritesSize,

		flags: 0,
	}

	journal := &Journal{
		disk:     disk,
		diskSize: diskSize,
		header:   header,
	}

	return journal, nil
}

func (j *Journal) Init() {

}
