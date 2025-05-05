package journal

import (
	"bdr/networking"
	"encoding/binary"
	"errors"
	"os"
	"unsafe"
	"fmt"
	"bdr/utils"
)

const (
	BdrMagic = 0x4244525F52504C43
)

const (
	JournalValidFlag = 1 << iota
)

var (
	ErrInvalidMagic = errors.New("invalid magic bytes in journal header")
	ErrInvalidSize  = errors.New("invalid section size")
	ErrIOFailure    = errors.New("I/O operation failed")
)

type Header struct {
	magic uint64

	bufWriteByteSize     uint64
	bufWritesCount       uint64
	bufWritesStartOffset uint64

	corrBlockByteSize     uint64
	corrBlocksCount       uint64
	corrBlocksStartOffset uint64

	flags uint64
}

func (j *Journal) String() string {
	return fmt.Sprintf(`
Actual offsets:
  Write offset: %d
  Correct offset: %d
Journal Header:
  Magic: 0x%016X
  Buffer Write Section:
    Entry Size: %d bytes
    Entry Count: %d
    Start Offset: %d
    Total Size: %d bytes
  Correct Block Section:
    Block Size: %d bytes
    Block Count: %d
    Start Offset: %d
    Total Size: %d bytes
  Valid: %v
  Flags: 0x%016X`,
	j.WriteOffset,
	j.CorrectOffset,
	j.header.magic,
	j.header.bufWriteByteSize,
	j.header.bufWritesCount,
	j.header.bufWritesStartOffset,
	j.header.GetBufWriteSectionByteSize(),
	j.header.corrBlockByteSize,
	j.header.corrBlocksCount,
	j.header.corrBlocksStartOffset,
	j.header.GetCorrBlockSectionByteSize(),
	j.header.IsValid(),
	j.header.flags)
}

func (h *Header) GetBufWriteSectionByteSize() uint64 {
	return h.bufWriteByteSize * h.bufWritesCount
}

func (h *Header) GetCorrBlockSectionByteSize() uint64 {
	return h.corrBlockByteSize * h.corrBlocksCount
}

func (h *Header) setValidFlag() {
	h.flags |= JournalValidFlag
}

func (h *Header) setInvalidFlag() {
	h.flags &= ^uint64(JournalValidFlag)
}

func (j *Journal) Invalidate() error {
	j.header.setInvalidFlag()

	return j.WriteHeader()
}

func (j *Journal) Validate() error {
	j.header.setValidFlag()

	return j.WriteHeader()
}

func (j *Journal) IsValid() bool {
	return j.header.IsValid()
}

func (h *Header) IsValid() bool {
	return (h.flags & JournalValidFlag) != 0
}

func (j *Journal) Close() error {
	if j.disk != nil {
		return j.disk.Close()
	}
	return nil
}

func (j *Journal) Equals(journal *Journal) bool {
	if journal == nil || j.header == nil || journal.header == nil {
		return false
	}

	return j.header.magic == journal.header.magic &&
		j.header.bufWriteByteSize == journal.header.bufWriteByteSize &&
		j.header.bufWritesCount == journal.header.bufWritesCount &&
		j.header.bufWritesStartOffset == journal.header.bufWritesStartOffset &&
		j.header.corrBlockByteSize == journal.header.corrBlockByteSize &&
		j.header.corrBlocksCount == journal.header.corrBlocksCount &&
		j.header.corrBlocksStartOffset == journal.header.corrBlocksStartOffset
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

	WriteOffset uint64
	CorrectOffset uint64

	header *Header
}

func (j *Journal) WriteHeader() error {
	headerBytes := make([]byte, GetHeaderByteSize())

	// Write the magic number
	binary.BigEndian.PutUint64(headerBytes[0:8], j.header.magic)

	// Write the buffer write section information
	binary.BigEndian.PutUint64(headerBytes[8:16], j.header.bufWriteByteSize)
	binary.BigEndian.PutUint64(headerBytes[16:24], j.header.bufWritesCount)
	binary.BigEndian.PutUint64(headerBytes[24:32], j.header.bufWritesStartOffset)

	// Write the correlation block section information
	binary.BigEndian.PutUint64(headerBytes[32:40], j.header.corrBlockByteSize)
	binary.BigEndian.PutUint64(headerBytes[40:48], j.header.corrBlocksCount)
	binary.BigEndian.PutUint64(headerBytes[48:56], j.header.corrBlocksStartOffset)

	// Write the flags
	binary.BigEndian.PutUint64(headerBytes[56:64], j.header.flags)

	_, err := j.disk.WriteAt(headerBytes, 0)
	if err != nil {
		return ErrIOFailure
	}

	err = j.disk.Sync()
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

func ValidateJournal(diskPath string, journal *Journal) (bool, *Journal, error) {
	readJournal, err := OpenJournal(diskPath)
	if err != nil {
		return false, nil, err
	}

	return journal.Equals(readJournal), readJournal, nil
}

func OpenJournal(diskPath string) (*Journal, error) {
	disk, err := os.OpenFile(diskPath, os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	header, err := ReadHeader(disk)
	if err != nil {
		disk.Close()
		return nil, err
	}

	if !VerifyMagic(header.magic) {
		disk.Close()
		return nil, ErrInvalidMagic
	}

	// Get disk size
	diskSize, err := utils.GetDeviceSize(diskPath)
	if err != nil {
		disk.Close()
		return nil, err
	}

	jrn := &Journal{
		disk:     disk,
		diskSize: diskSize,
		header:   header,
	}

	firstWrite, err := jrn.FindFirstAvailableBufferWrite()
	if err != nil {
		disk.Close()
		return nil, err
	}

	firstBuffer, err := jrn.FindFirstAvailableCorrectBlock()
	if err != nil {
		disk.Close()
		return nil, err
	}

	jrn.WriteOffset = firstWrite
	jrn.CorrectOffset = firstBuffer

	return jrn, nil
}

func serializeCorrectBlockInfo(info *networking.CorrectBlockInfo, buffer []byte) {
	binary.BigEndian.PutUint64(buffer[0:8], info.Offset)
	binary.BigEndian.PutUint32(buffer[8:12], info.Size)

	// Copy the data if it exists
	if info.Size > 0 && len(info.Data) > 0 {
		dataSize := uint32(len(info.Data))
		if dataSize > info.Size {
			dataSize = info.Size
		}
		copy(buffer[12:12+dataSize], info.Data[:dataSize])
	}
}

func deserializeCorrectBlockInfo(buffer []byte) *networking.CorrectBlockInfo {
	info := &networking.CorrectBlockInfo{}
	info.Offset = binary.BigEndian.Uint64(buffer[0:8])
	info.Size = binary.BigEndian.Uint32(buffer[8:12])

	if info.Size > 0 {
		info.Data = make([]byte, info.Size)
		copy(info.Data, buffer[12:12+info.Size])
	}

	return info
}

func serializeWriteInfo(info *networking.WriteInfo, buffer []byte) {
	binary.BigEndian.PutUint64(buffer[0:8], info.Offset)
	binary.BigEndian.PutUint32(buffer[8:12], info.Size)

	if info.Size > 0 && len(info.Data) > 0 {
		dataSize := uint32(len(info.Data))
		if dataSize > info.Size {
			dataSize = info.Size
		}
		copy(buffer[12:12+dataSize], info.Data[:dataSize])
	}
}

func deserializeWriteInfo(buffer []byte) *networking.WriteInfo {
	info := &networking.WriteInfo{}
	info.Offset = binary.BigEndian.Uint64(buffer[0:8])
	info.Size = binary.BigEndian.Uint32(buffer[8:12])

	if info.Size > 0 {
		info.Data = make([]byte, info.Size)
		copy(info.Data, buffer[12:12+info.Size])
	}

	return info
}

func (j *Journal) GetJournalCoverPercentage(targetDiskSize uint64) float64 {
    corrBlockCoverage := j.header.GetCorrBlockSectionByteSize()

    return (float64(corrBlockCoverage) / float64(targetDiskSize)) * 100
}

func NewJournal(diskPath string, sectionBufWritesSize uint64, bufWriteByteSize uint64, corrBlockByteSize uint64) (*Journal, error) {
	disk, err := os.OpenFile(diskPath, os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	diskSize, err := utils.GetDeviceSize(diskPath)
	if err != nil {
		disk.Close()
		return nil, err
	}

	headerSize := GetHeaderByteSize()
	if sectionBufWritesSize > diskSize-headerSize || sectionBufWritesSize == 0 {
		disk.Close()
		return nil, ErrInvalidSize
	}

	if corrBlockByteSize == 0 || bufWriteByteSize == 0 {
		disk.Close()
		return nil, ErrInvalidSize
	}

	sectionCorBlocksSize := diskSize - headerSize - sectionBufWritesSize

	header := &Header{
		magic: BdrMagic,

		bufWriteByteSize:     bufWriteByteSize,
		bufWritesCount:       sectionBufWritesSize / bufWriteByteSize,
		bufWritesStartOffset: headerSize,

		corrBlockByteSize:     corrBlockByteSize,
		corrBlocksCount:       sectionCorBlocksSize / corrBlockByteSize,
		corrBlocksStartOffset: headerSize + sectionBufWritesSize,

		flags: 0,
	}

	journal := &Journal{
		disk:     disk,
		diskSize: diskSize,
		WriteOffset: sectionBufWritesSize / bufWriteByteSize,
		CorrectOffset: sectionCorBlocksSize / corrBlockByteSize,
		header:   header,
	}

	return journal, nil
}

func GetInvalidCorrectBlock() *networking.CorrectBlockInfo {
	return &networking.CorrectBlockInfo{
		Size: 0,
	}
}

func (j *Journal) ClearCorrectBlockSectionTo(correctBlockOff uint64) error {
	if correctBlockOff > j.header.corrBlocksCount {
		return ErrInvalidSize
	}

	invalidCorrectBlock := GetInvalidCorrectBlock()
	blockData := make([]byte, j.header.corrBlockByteSize)

	serializeCorrectBlockInfo(invalidCorrectBlock, blockData)

	for i := uint64(0); i < correctBlockOff; i++ {
		offset := j.header.corrBlocksStartOffset + uint64(i)*j.header.corrBlockByteSize

		_, err := j.disk.WriteAt(blockData, int64(offset))
		if err != nil {
			return ErrIOFailure
		}
	}

	return nil
}

func (j *Journal) ClearWriteSectionTo(bufferWriteOff uint64) error {
	if bufferWriteOff > j.header.bufWritesCount {
		return ErrInvalidSize
	}

	invalidWrite := GetInvalidWrite()
	writeData := make([]byte, j.header.bufWriteByteSize)

	serializeWriteInfo(invalidWrite, writeData)

	for i := uint64(0); i < bufferWriteOff; i++ {
		offset := j.header.bufWritesStartOffset + uint64(i)*j.header.bufWriteByteSize

		_, err := j.disk.WriteAt(writeData, int64(offset))
		if err != nil {
			return ErrIOFailure
		}
	}
	return nil
}


func (j *Journal) ResetTo(bufferWriteOff uint64, correctBlockOff uint64) error {
	err := j.Invalidate()
	if err != nil {
		return err
	}

	err = j.ClearWriteSectionTo(bufferWriteOff)
	if err != nil {
		return err
	}

	err = j.ClearCorrectBlockSectionTo(correctBlockOff)
	if err != nil {
		return err
	}

	j.WriteOffset = 0
	j.CorrectOffset = 0

	return nil

}

func (j *Journal) Reset() error {
	err := j.Invalidate()
	if err != nil {
		return err
	}

	err = j.ClearWriteSection()
	if err != nil {
		return err
	}

	err = j.ClearCorrectBlockSection()
	if err != nil {
		return err
	}

	j.WriteOffset = 0
	j.CorrectOffset = 0

	return nil
}

func (j *Journal) ClearCorrectBlockSection() error {
	invalidCorrectBlock := GetInvalidCorrectBlock()
	blockData := make([]byte, j.header.corrBlockByteSize)

	serializeCorrectBlockInfo(invalidCorrectBlock, blockData)

	for i := uint64(0); i < j.header.corrBlocksCount; i++ {
		offset := j.header.corrBlocksStartOffset + uint64(i)*j.header.corrBlockByteSize

		_, err := j.disk.WriteAt(blockData, int64(offset))
		if err != nil {
			return ErrIOFailure
		}
	}

	return nil
}

func GetInvalidWrite() *networking.WriteInfo {
	return &networking.WriteInfo{
		Size: 0,
	}
}

func (j *Journal) ClearWriteSection() error {
	invalidWrite := GetInvalidWrite()
	writeData := make([]byte, j.header.bufWriteByteSize)

	serializeWriteInfo(invalidWrite, writeData)

	for i := uint64(0); i < j.header.bufWritesCount; i++ {
		offset := j.header.bufWritesStartOffset + uint64(i)*j.header.bufWriteByteSize

		_, err := j.disk.WriteAt(writeData, int64(offset))
		if err != nil {
			return ErrIOFailure
		}
	}

	return nil
}

func (j *Journal) InitWithoutClearing() error {
	return j.Invalidate()
}

func (j *Journal) Init() error {
	if err := j.ClearWriteSection(); err != nil {
		return err
	}

	if err := j.ClearCorrectBlockSection(); err != nil {
		return err
	}

	return j.Invalidate()
}

func (j *Journal) WriteCorrectBlock(index uint64, block *networking.CorrectBlockInfo) error {
	if index >= j.header.corrBlocksCount {
		return ErrInvalidSize
	}

	blockData := make([]byte, j.header.corrBlockByteSize)

	serializeCorrectBlockInfo(block, blockData)

	offset := j.header.corrBlocksStartOffset + index*j.header.corrBlockByteSize
	_, err := j.disk.WriteAt(blockData, int64(offset))
	if err != nil {
		return ErrIOFailure
	}

	return j.disk.Sync()
}

func (j *Journal) ReadCorrectBlock(index uint64) (*networking.CorrectBlockInfo, error) {
	if index >= j.header.corrBlocksCount {
		return nil, ErrInvalidSize
	}

	blockData := make([]byte, j.header.corrBlockByteSize)

	offset := j.header.corrBlocksStartOffset + index*j.header.corrBlockByteSize
	_, err := j.disk.ReadAt(blockData, int64(offset))
	if err != nil {
		return nil, ErrIOFailure
	}

	block := deserializeCorrectBlockInfo(blockData)

	return block, nil
}

func (j *Journal) WriteBufferWrite(index uint64, write *networking.WriteInfo) error {
	if index >= j.header.bufWritesCount {
		return ErrInvalidSize
	}

	writeData := make([]byte, j.header.bufWriteByteSize)

	serializeWriteInfo(write, writeData)

	offset := j.header.bufWritesStartOffset + index*j.header.bufWriteByteSize
	_, err := j.disk.WriteAt(writeData, int64(offset))
	if err != nil {
		return ErrIOFailure
	}

	return j.disk.Sync()
}

func (j *Journal) ReadBufferWrite(index uint64) (*networking.WriteInfo, error) {
	if index >= j.header.bufWritesCount {
		return nil, ErrInvalidSize
	}

	writeData := make([]byte, j.header.bufWriteByteSize)

	offset := j.header.bufWritesStartOffset + index*j.header.bufWriteByteSize
	_, err := j.disk.ReadAt(writeData, int64(offset))
	if err != nil {
		return nil, ErrIOFailure
	}

	write := deserializeWriteInfo(writeData)

	return write, nil
}

func (j *Journal) FindFirstAvailableBufferWrite() (uint64, error) {
	for i := uint64(0); i < j.header.bufWritesCount; i++ {
		write, err := j.ReadBufferWrite(i)
		if err != nil {
			return 0, err
		}

		if !write.IsValid() {
			return i, nil
		}
	}

	return 0, errors.New("no available buffer write slots")
}

func (j *Journal) FindFirstAvailableCorrectBlock() (uint64, error) {
	for i := uint64(0); i < j.header.corrBlocksCount; i++ {
		block, err := j.ReadCorrectBlock(i)
		if err != nil {
			return 0, err
		}

		if !block.IsValid() {
			return i, nil
		}
	}

	return 0, errors.New("no available correct block slots")
}
