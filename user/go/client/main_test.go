package main

import (
	"os"
	"syscall"
	"testing"
	"unsafe"
)

func TestMmap(t *testing.T) {
	fd, err := os.OpenFile("/dev/bdr-1", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("Skipping device tests: %v", err)
		return
	}
	defer fd.Close()

	var targetInfo TargetInfo
	err = ioctl(fd.Fd(), BDR_CMD_GET_TARGET_INFO, uintptr(unsafe.Pointer(&targetInfo)))
	if err != nil {
		t.Fatalf("MMAP failed: %v", err)
	}

	t.Run("(MMAP) Buffer consistency", func(t *testing.T) {
		// mmap shared buffer
		buf, err := syscall.Mmap(int(fd.Fd()), 0, int(targetInfo.BufferByteSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
		if err != nil {
			t.Fatalf("MMAP failed: %v", err)
		}

		var startOffset offset_t = 0
		var endOffset offset_t = offset_t(targetInfo.BufferByteSize)
		var jumpOffset offset_t = 234
		for startOffset < endOffset {
			err = ioctl(fd.Fd(), BDR_CMD_WRITE_TEST_VALUE, uintptr(unsafe.Pointer(&startOffset)))
			if err != nil {
				t.Fatalf("Failed to write test value: %v", err)
			}

			if buf[startOffset] != 0xAA {
				t.Errorf("Unexpected value at offset %d: got %x, want 0xAA", startOffset, buf[startOffset])
			}

			startOffset += jumpOffset
		}
	})
}
