package utils

import (
	"fmt"
	"os"
	"testing"
)

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
