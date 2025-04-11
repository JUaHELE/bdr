package utils

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func Bit(nr uint) uint32 { return 1 << nr }

func PrintOnError(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "[ERROR]: ", err)
	}
}

func ExitOnError(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "[ERROR]: ", err)
		os.Exit(1)
	}
}

func AssertEqual(t *testing.T, expected, actual uintptr, msg string) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %d, got %d", msg, expected, actual)
	}
}

func AssertInRange(t *testing.T, value, min, max uint64, name string) {
	t.Helper()
	if value < min || value > max {
		t.Errorf("%s (%d) not in range [%d, %d]", name, value, min, max)
	}
}

func ChanHasTerminated(termChan chan struct{}) bool {
	select {
	case <-termChan:
		return true
	default:
		return false
	}
}

const BLKSSZGET = 0x1268

func GetSectorSize(device string) (uint32, error) {
	// Open the device file
	file, err := os.Open(device)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	// Prepare variable to store sector size
	var sectorSize uint32

	// Call ioctl
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), BLKSSZGET, uintptr(unsafe.Pointer(&sectorSize)))
	if errno != 0 {
		return 0, fmt.Errorf("ioctl failed: %v", errno)
	}

	return sectorSize, nil
}

const BLKGETSIZE64 = 0x80081272

func GetDeviceSize(device string) (uint64, error) {
	file, err := os.Open(device)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var size uint64

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), BLKGETSIZE64, uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return 0, fmt.Errorf("ioctl failed: %v", errno)
	}

	return size, nil
}

func GetDeviceSizeForTest(path string) (uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return 0, err
	}

	return uint64(fileInfo.Size()), nil
}

func HexDump(data []byte) {
	const bytesPerLine = 16
	for i := 0; i < len(data); i += bytesPerLine {
		fmt.Printf("%08x  ", i)

		for j := 0; j < bytesPerLine; j++ {
			if i+j < len(data) {
				fmt.Printf("%02x ", data[i+j])
			} else {
				fmt.Print("   ")
			}
		}

		fmt.Print(" ")
		for j := 0; j < bytesPerLine; j++ {
			if i+j < len(data) {
				b := data[i+j]
				if b >= 32 && b <= 126 {
					fmt.Printf("%c", b)
				} else {
					fmt.Print(".")
				}
			}
		}
		fmt.Println()
	}
}

func PrintBitmap(bitmap []byte, maxBlocks uint64) string {
	if maxBlocks == 0 || maxBlocks > uint64(len(bitmap)*8) {
		maxBlocks = uint64(len(bitmap) * 8)
	}

	var builder strings.Builder
	for i := uint64(0); i < maxBlocks; i++ {
		byteIndex := i / 8
		bitPosition := i % 8

		if (bitmap[byteIndex] & (1 << (7 - bitPosition))) != 0 {
			builder.WriteRune('1')
		} else {
			builder.WriteRune('0')
		}

		if (i+1)%64 == 0 && i+1 < maxBlocks {
			builder.WriteRune(' ')
		}
	}

	return builder.String()
}
