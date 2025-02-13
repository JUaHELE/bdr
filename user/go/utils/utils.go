package utils

import (
	"fmt"
	"os"
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
