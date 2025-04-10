package journal

import (
	"os"
	"path/filepath"
	"testing"
	"bdr/networking"
)

const (
	TestDiskSize        = 1024 * 1024
	BufferWriteSize     = 4096        // 4KB per buffer write
	CorrectBlockSize    = 4096        // 4KB per correct block
	TestSectionBufWrites = 512 * 1024 // 512KB for buffer writes section
)

func createTempJournalFile(t *testing.T) string {
	tempDir := t.TempDir()
	journalPath := filepath.Join(tempDir, "test_journal.dat")
	
	file, err := os.Create(journalPath)
	if err != nil {
		t.Fatalf("Failed to create temp journal file: %v", err)
	}
	
	err = file.Truncate(int64(TestDiskSize))
	if err != nil {
		t.Fatalf("Failed to resize temp journal file: %v", err)
	}
	
	file.Close()
	return journalPath
}

func createTestJournal(t *testing.T) (*Journal, string) {
	journalPath := createTempJournalFile(t)
	
	journal, err := NewJournal(journalPath, TestSectionBufWrites, BufferWriteSize, CorrectBlockSize)
	if err != nil {
		t.Fatalf("Failed to create new journal: %v", err)
	}
	
	err = journal.Init()
	if err != nil {
		t.Fatalf("Failed to initialize journal: %v", err)
	}
	
	return journal, journalPath
}

func createTestWriteInfo(offset uint64, size uint32) *networking.WriteInfo {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}
	
	return &networking.WriteInfo{
		Offset: offset,
		Size:   size,
		Data:   data,
	}
}

func createTestCorrectBlockInfo(offset uint64, size uint32) *networking.CorrectBlockInfo {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(255 - (i % 256))
	}
	
	return &networking.CorrectBlockInfo{
		Offset: offset,
		Size:   size,
		Data:   data,
	}
}

func TestJournalCreationAndInit(t *testing.T) {
	journal, journalPath := createTestJournal(t)
	defer journal.Close()
	defer os.Remove(journalPath)
	
	if journal.header.IsValid() {
		t.Error("Journal shouldn't be valid after initialization")
	}
	
	if journal.header.magic != BdrMagic {
		t.Errorf("Expected magic %x, got %x", BdrMagic, journal.header.magic)
	}
	
	expectedBufWritesCount := TestSectionBufWrites / BufferWriteSize
	if journal.header.bufWritesCount != uint64(expectedBufWritesCount) {
		t.Errorf("Expected buffer writes count %d, got %d", expectedBufWritesCount, journal.header.bufWritesCount)
	}
	
	expectedCorrBlocksCount := (TestDiskSize - GetHeaderByteSize() - TestSectionBufWrites) / CorrectBlockSize
	if journal.header.corrBlocksCount != expectedCorrBlocksCount {
		t.Errorf("Expected correct blocks count %d, got %d", expectedCorrBlocksCount, journal.header.corrBlocksCount)
	}
	
	// Verify that sections are properly initialized with invalid entries
	bufWriteIndex, err := journal.FindFirstAvailableBufferWrite()
	if err != nil {
		t.Errorf("Failed to find available buffer write: %v", err)
	}
	if bufWriteIndex != 0 {
		t.Errorf("Expected first available buffer write at index 0, got %d", bufWriteIndex)
	}
	
	corrBlockIndex, err := journal.FindFirstAvailableCorrectBlock()
	if err != nil {
		t.Errorf("Failed to find available correct block: %v", err)
	}
	if corrBlockIndex != 0 {
		t.Errorf("Expected first available correct block at index 0, got %d", corrBlockIndex)
	}
}

func TestJournalHeader(t *testing.T) {
	journal, journalPath := createTestJournal(t)
	defer os.Remove(journalPath)
	
	err := journal.Validate()
	if err != nil {
		t.Fatalf("Failed to write header: %v", err)
	}
	journal.Close()
	
	reopenedJournal, err := OpenJournal(journalPath)
	if err != nil {
		t.Fatalf("Failed to reopen journal: %v", err)
	}
	defer reopenedJournal.Close()
	
	if !reopenedJournal.header.IsValid() {
		t.Error("Reopened journal should be valid")
	}
	
	if !journal.Equals(reopenedJournal) {
		t.Error("Original and reopened journals should be equal")
	}
	
	isValid, err := ValidateJournal(journalPath, journal)
	if err != nil {
		t.Fatalf("Journal validation failed: %v", err)
	}
	if !isValid {
		t.Error("Journal validation should return true for equal journals")
	}
}

func TestBufferWriteOperations(t *testing.T) {
	journal, journalPath := createTestJournal(t)
	defer journal.Close()
	defer os.Remove(journalPath)
	
	testWrite := createTestWriteInfo(0x1000, 1024)
	
	err := journal.WriteBufferWrite(0, testWrite)
	if err != nil {
		t.Fatalf("Failed to write buffer write: %v", err)
	}
	
	readWrite, err := journal.ReadBufferWrite(0)
	if err != nil {
		t.Fatalf("Failed to read buffer write: %v", err)
	}
	
	if readWrite.Offset != testWrite.Offset {
		t.Errorf("Expected offset %d, got %d", testWrite.Offset, readWrite.Offset)
	}

	if readWrite.Size != testWrite.Size {
		t.Errorf("Expected size %d, got %d", testWrite.Size, readWrite.Size)
	}
	
	for i := range testWrite.Data {
		if i >= len(readWrite.Data) {
			t.Fatalf("Read data too short, expected length %d, got %d", len(testWrite.Data), len(readWrite.Data))
		}
		if readWrite.Data[i] != testWrite.Data[i] {
			t.Errorf("Data mismatch at index %d: expected %d, got %d", i, testWrite.Data[i], readWrite.Data[i])
		}
	}
	
	bufWriteIndex, err := journal.FindFirstAvailableBufferWrite()
	if err != nil {
		t.Fatalf("Failed to find available buffer write: %v", err)
	}
	if bufWriteIndex != 1 {
		t.Errorf("Expected first available buffer write at index 1, got %d", bufWriteIndex)
	}
	
	_, err = journal.ReadBufferWrite(journal.header.bufWritesCount)
	if err == nil {
		t.Error("Reading buffer write with invalid index should fail")
	}
	
	err = journal.WriteBufferWrite(journal.header.bufWritesCount, testWrite)
	if err == nil {
		t.Error("Writing buffer write with invalid index should fail")
	}
}

func TestCorrectBlockOperations(t *testing.T) {
	journal, journalPath := createTestJournal(t)
	defer journal.Close()
	defer os.Remove(journalPath)
	
	testBlock := createTestCorrectBlockInfo(0x2000, 2048)
	
	err := journal.WriteCorrectBlock(0, testBlock)
	if err != nil {
		t.Fatalf("Failed to write correct block: %v", err)
	}
	
	readBlock, err := journal.ReadCorrectBlock(0)
	if err != nil {
		t.Fatalf("Failed to read correct block: %v", err)
	}
	
	if readBlock.Offset != testBlock.Offset {
		t.Errorf("Expected offset %d, got %d", testBlock.Offset, readBlock.Offset)
	}
	if readBlock.Size != testBlock.Size {
		t.Errorf("Expected size %d, got %d", testBlock.Size, readBlock.Size)
	}
	
	for i := range testBlock.Data {
		if i >= len(readBlock.Data) {
			t.Fatalf("Read data too short, expected length %d, got %d", len(testBlock.Data), len(readBlock.Data))
		}
		if readBlock.Data[i] != testBlock.Data[i] {
			t.Errorf("Data mismatch at index %d: expected %d, got %d", i, testBlock.Data[i], readBlock.Data[i])
		}
	}
	
	corrBlockIndex, err := journal.FindFirstAvailableCorrectBlock()
	if err != nil {
		t.Fatalf("Failed to find available correct block: %v", err)
	}
	if corrBlockIndex != 1 {
		t.Errorf("Expected first available correct block at index 1, got %d", corrBlockIndex)
	}
	
	_, err = journal.ReadCorrectBlock(journal.header.corrBlocksCount)
	if err == nil {
		t.Error("Reading correct block with invalid index should fail")
	}
	
	err = journal.WriteCorrectBlock(journal.header.corrBlocksCount, testBlock)
	if err == nil {
		t.Error("Writing correct block with invalid index should fail")
	}
}

func TestJournalValidation(t *testing.T) {
	journal, journalPath := createTestJournal(t)
	defer os.Remove(journalPath)
	
	if journal.header.IsValid() {
		t.Error("Journal shouldn't be valid after initialization")
	}
	
	err := journal.Validate()
	if err != nil {
		t.Fatalf("Failed to invalidate journal: %v", err)
	}
	
	// Check if the journal is now invalid
	if !journal.header.IsValid() {
		t.Error("Journal should be valid after validation")
	}
	
	journal.Close()
	
	reopenedJournal, err := OpenJournal(journalPath)
	if err != nil {
		t.Fatalf("Failed to reopen journal: %v", err)
	}
	defer reopenedJournal.Close()
	
	if !reopenedJournal.header.IsValid() {
		t.Error("Reopened journal should still be valid")
	}
	
	err = reopenedJournal.Invalidate()
	if err != nil {
		t.Fatalf("Failed to validate journal: %v", err)
	}
	
	if reopenedJournal.header.IsValid() {
		t.Error("Journal should be invalid")
	}
}

// Test filling up buffer writes and correct blocks
func TestJournalCapacity(t *testing.T) {
	journal, journalPath := createTestJournal(t)
	defer journal.Close()
	defer os.Remove(journalPath)
	
	const testEntries = 10
	
	for i := uint64(0); i < testEntries; i++ {
		testWrite := createTestWriteInfo(i*0x1000, 1024)
		err := journal.WriteBufferWrite(i, testWrite)
		if err != nil {
			t.Fatalf("Failed to write buffer write at index %d: %v", i, err)
		}
	}
	
	bufWriteIndex, err := journal.FindFirstAvailableBufferWrite()
	if err != nil {
		t.Fatalf("Failed to find available buffer write: %v", err)
	}
	if bufWriteIndex != testEntries {
		t.Errorf("Expected first available buffer write at index %d, got %d", testEntries, bufWriteIndex)
	}
	
	for i := uint64(0); i < testEntries; i++ {
		testBlock := createTestCorrectBlockInfo(i*0x2000, 2048)
		err := journal.WriteCorrectBlock(i, testBlock)
		if err != nil {
			t.Fatalf("Failed to write correct block at index %d: %v", i, err)
		}
	}
	
	corrBlockIndex, err := journal.FindFirstAvailableCorrectBlock()
	if err != nil {
		t.Fatalf("Failed to find available correct block: %v", err)
	}
	if corrBlockIndex != testEntries {
		t.Errorf("Expected first available correct block at index %d, got %d", testEntries, corrBlockIndex)
	}
	
	err = journal.ClearWriteSection()
	if err != nil {
		t.Fatalf("Failed to clear write section: %v", err)
	}
	
	bufWriteIndex, err = journal.FindFirstAvailableBufferWrite()
	if err != nil {
		t.Fatalf("Failed to find available buffer write after clearing: %v", err)
	}
	if bufWriteIndex != 0 {
		t.Errorf("Expected first available buffer write at index 0 after clearing, got %d", bufWriteIndex)
	}
	
	err = journal.ClearCorrectBlockSection()
	if err != nil {
		t.Fatalf("Failed to clear correct block section: %v", err)
	}
	
	corrBlockIndex, err = journal.FindFirstAvailableCorrectBlock()
	if err != nil {
		t.Fatalf("Failed to find available correct block after clearing: %v", err)
	}
	if corrBlockIndex != 0 {
		t.Errorf("Expected first available correct block at index 0 after clearing, got %d", corrBlockIndex)
	}
}

func TestJournalErrors(t *testing.T) {
	journalPath := createTempJournalFile(t)
	defer os.Remove(journalPath)
	
	_, err := NewJournal(journalPath, 0, BufferWriteSize, CorrectBlockSize)
	if err == nil {
		t.Error("Creating journal with invalid section size should fail")
	}
	
	_, err = NewJournal(journalPath, TestSectionBufWrites, 0, CorrectBlockSize)
	if err == nil {
		t.Error("Creating journal with invalid buffer write size should fail")
	}
	
	_, err = NewJournal(journalPath, TestSectionBufWrites, BufferWriteSize, 0)
	if err == nil {
		t.Error("Creating journal with invalid correct block size should fail")
	}
	
	_, err = OpenJournal("non_existent_file.dat")
	if err == nil {
		t.Error("Opening non-existent journal should fail")
	}
	
	file, _ := os.OpenFile(journalPath, os.O_RDWR, 0644)
	corruptedHeader := make([]byte, 8)
	file.WriteAt(corruptedHeader, 0) // Write zeros to corrupt magic
	file.Close()
	
	journal, err := NewJournal(journalPath, TestSectionBufWrites, BufferWriteSize, CorrectBlockSize)
	if err != nil {
		t.Fatalf("Failed to create journal: %v", err)
	}
	defer journal.Close()
	
	if !VerifyMagic(journal.header.magic) {
		t.Error("Header magic verification should fail with corrupted header")
	}
}

func TestSectionSizes(t *testing.T) {
	journal, journalPath := createTestJournal(t)
	defer journal.Close()
	defer os.Remove(journalPath)
	
	expectedBufWriteSectionSize := journal.header.bufWriteByteSize * journal.header.bufWritesCount
	calculatedSize := journal.header.GetBufWriteSectionByteSize()
	if calculatedSize != expectedBufWriteSectionSize {
		t.Errorf("Expected buffer write section size %d, got %d", expectedBufWriteSectionSize, calculatedSize)
	}
	
	expectedCorrBlockSectionSize := journal.header.corrBlockByteSize * journal.header.corrBlocksCount
	calculatedSize = journal.header.GetCorrBlockSectionByteSize()
	if calculatedSize != expectedCorrBlockSectionSize {
		t.Errorf("Expected correct block section size %d, got %d", expectedCorrBlockSectionSize, calculatedSize)
	}
}

func TestSerialization(t *testing.T) {
	writeInfo := createTestWriteInfo(0x1000, 1024)
	writeBuffer := make([]byte, BufferWriteSize)
	
	serializeWriteInfo(writeInfo, writeBuffer)
	deserializedWrite := deserializeWriteInfo(writeBuffer)
	
	if deserializedWrite.Offset != writeInfo.Offset {
		t.Errorf("WriteInfo serialization: expected offset %d, got %d", writeInfo.Offset, deserializedWrite.Offset)
	}
	if deserializedWrite.Size != writeInfo.Size {
		t.Errorf("WriteInfo serialization: expected size %d, got %d", writeInfo.Size, deserializedWrite.Size)
	}
	
	corrBlockInfo := createTestCorrectBlockInfo(0x2000, 2048)
	blockBuffer := make([]byte, CorrectBlockSize)
	
	serializeCorrectBlockInfo(corrBlockInfo, blockBuffer)
	deserializedBlock := deserializeCorrectBlockInfo(blockBuffer)
	
	if deserializedBlock.Offset != corrBlockInfo.Offset {
		t.Errorf("CorrectBlockInfo serialization: expected offset %d, got %d", corrBlockInfo.Offset, deserializedBlock.Offset)
	}
	if deserializedBlock.Size != corrBlockInfo.Size {
		t.Errorf("CorrectBlockInfo serialization: expected size %d, got %d", corrBlockInfo.Size, deserializedBlock.Size)
	}
}

func TestInvalidEntries(t *testing.T) {
	invalidWrite := GetInvalidWrite()
	if invalidWrite.IsValid() {
		t.Error("Invalid write should not be valid")
	}
	
	invalidCorrectBlock := GetInvalidCorrectBlock()
	if invalidCorrectBlock.IsValid() {
		t.Error("Invalid correct block should not be valid")
	}
}

func BenchmarkJournalOperations(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "journal_bench")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)
	
	journalPath := filepath.Join(tempDir, "bench_journal.dat")
	file, err := os.Create(journalPath)
	if err != nil {
		b.Fatalf("Failed to create journal file: %v", err)
	}
	file.Truncate(int64(TestDiskSize))
	file.Close()
	
	journal, err := NewJournal(journalPath, TestSectionBufWrites, BufferWriteSize, CorrectBlockSize)
	if err != nil {
		b.Fatalf("Failed to create journal: %v", err)
	}
	
	err = journal.Init()
	if err != nil {
		b.Fatalf("Failed to initialize journal: %v", err)
	}
	defer journal.Close()
	
	b.Run("WriteBufferWrite", func(b *testing.B) {
		testWrite := createTestWriteInfo(0x1000, 1024)
		
		for i := 0; i < b.N; i++ {
			index := uint64(i % int(journal.header.bufWritesCount))
			journal.WriteBufferWrite(index, testWrite)
		}
	})
	
	b.Run("ReadBufferWrite", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			index := uint64(i % int(journal.header.bufWritesCount))
			journal.ReadBufferWrite(index)
		}
	})
	
	b.Run("WriteCorrectBlock", func(b *testing.B) {
		testBlock := createTestCorrectBlockInfo(0x2000, 2048)
		
		for i := 0; i < b.N; i++ {
			index := uint64(i % int(journal.header.corrBlocksCount))
			journal.WriteCorrectBlock(index, testBlock)
		}
	})
	
	b.Run("ReadCorrectBlock", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			index := uint64(i % int(journal.header.corrBlocksCount))
			journal.ReadCorrectBlock(index)
		}
	})
}
