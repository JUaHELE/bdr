package main

import (
	"testing"
	"unsafe"
	"os"
	"bdr/utils"
)

type MockFd struct {
	fd uintptr
}

func TestIoctl(t *testing.T) {
	// skip this test if we're not running as root or don't have access to devices
	if testing.Short() {
		t.Skip("Skipping ioctl test in short mode")
	}

	tests := []struct {
		name        string
		request     uintptr
		arg         uintptr
		expectError bool
	}{
		{
			name:        "invalid fd",
			request:     BDR_CMD_GET_BUFFER_INFO,
			arg:         0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFd := MockFd{fd: ^uintptr(0)} // invalid FD
			err := ioctl(mockFd.fd, tt.request, tt.arg)

			if tt.expectError && err == nil {
				t.Errorf("%s: expected error but got none", tt.name)
			}
			if !tt.expectError && err != nil {
				t.Errorf("%s: unexpected error: %v", tt.name, err)
			}
		})
	}
}

func isValidPageSize(size uint64) bool {
	validSizes := []uint64{4096, 8192, 16384, 65536}
	for _, valid := range validSizes {
		if size == valid {
			return true
		}
	}
	return false
}

// TestDeviceIoctls tests the actual device IOCTLs
func TestDeviceIoctls(t *testing.T) {
	// try to open the device, skip if the device doesnt exist
	fd, err := os.OpenFile(devicePath, os.O_RDWR, 0)
	if err != nil {
		t.Skipf("Skipping device tests: %v", err)
		return
	}
	defer fd.Close()

	t.Run("GET_BUFFER_INFO", func(t *testing.T) {
		var bufferInfo BufferInfo
		err := ioctl(fd.Fd(), BDR_CMD_GET_BUFFER_INFO, uintptr(unsafe.Pointer(&bufferInfo)))
		if err != nil {
			t.Fatalf("GET_BUFFER_INFO failed: %v", err)
		}

		// check that values are within expected ranges
		if bufferInfo.MaxWrites == 0 {
			t.Error("MaxWrites should not be 0")
		}

		// check Offset is within range 0-MaxWrites
		utils.AssertInRange(t, bufferInfo.Offset, 0, bufferInfo.MaxWrites, "Offset")

		// check Length is within range 0-MaxWrites
		utils.AssertInRange(t, bufferInfo.Length, 0, bufferInfo.MaxWrites, "Length")

		// check Last is within range 0-MaxWrites
		utils.AssertInRange(t, bufferInfo.Last, 0, bufferInfo.MaxWrites, "Last")

		t.Logf("BufferInfo: Offset=%d, Length=%d, Last=%d, Flags=%d, MaxWrites=%d",
			bufferInfo.Offset, bufferInfo.Length, bufferInfo.Last, bufferInfo.Flags, bufferInfo.MaxWrites)
	})

	t.Run("GET_TARGET_INFO", func(t *testing.T) {
		var targetInfo TargetInfo
		err := ioctl(fd.Fd(), BDR_CMD_GET_TARGET_INFO, uintptr(unsafe.Pointer(&targetInfo)))
		if err != nil {
			t.Fatalf("GET_TARGET_INFO failed: %v", err)
		}

		// check PageSize
		if !isValidPageSize(targetInfo.PageSize) {
			t.Errorf("Unexpected page size: %d", targetInfo.PageSize)
		}

		// writeInfoSize and BufferByteSize should be non-zero
		if targetInfo.WriteInfoSize == 0 {
			t.Error("WriteInfoSize should not be 0")
		}

		// again...
		if targetInfo.BufferByteSize == 0 {
			t.Error("BufferByteSize should not be 0")
		}

		t.Logf("TargetInfo: PageSize=%d, WriteInfoSize=%d, BufferByteSize=%d",
			targetInfo.PageSize, targetInfo.WriteInfoSize, targetInfo.BufferByteSize)
	})

	t.Run("GET_STATUS", func(t *testing.T) {
		var status enum_t
		err := ioctl(fd.Fd(), BDR_CMD_GET_STATUS, uintptr(unsafe.Pointer(&status)))
		if err != nil {
			t.Fatalf("GET_STATUS failed: %v", err)
		}

		t.Logf("Status: %d", status)
	})
}
