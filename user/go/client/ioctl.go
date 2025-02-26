package main

import (
	"syscall"
	"unsafe"
	"fmt"
)

const (
	_IOC_NRBITS   = 8
	_IOC_TYPEBITS = 8
	_IOC_SIZEBITS = 14
	_IOC_DIRBITS  = 2

	_IOC_NRSHIFT   = 0
	_IOC_TYPESHIFT = (_IOC_NRSHIFT + _IOC_NRBITS)
	_IOC_SIZESHIFT = (_IOC_TYPESHIFT + _IOC_TYPEBITS)
	_IOC_DIRSHIFT  = (_IOC_SIZESHIFT + _IOC_SIZEBITS)

	_IOC_NONE  = 0
	_IOC_WRITE = 1
	_IOC_READ  = 2
)

// helper constructor function
func _IOC(dir, t, nr, size uintptr) uintptr {
	return (dir << _IOC_DIRSHIFT) | (t << _IOC_TYPESHIFT) | (nr << _IOC_NRSHIFT) | (size << _IOC_SIZESHIFT)
}

// constructs an ioctl request with no data transfer
func _IO(t, nr uintptr) uintptr {
	return _IOC(_IOC_NONE, t, nr, 0)
}

// constructs a read-only ioctl request
func _IOR(t, nr, size uintptr) uintptr {
	return _IOC(_IOC_READ, t, nr, size)
}

// _IOW constructs a write-only ioctl request
func _IOW(t, nr, size uintptr) uintptr {
	return _IOC(_IOC_WRITE, t, nr, size)
}

// constructs a read-write ioctl request
func _IOWR(t, nr, size uintptr) uintptr {
	return _IOC(_IOC_READ|_IOC_WRITE, t, nr, size)
}

const (
	BDR_MAGIC = 'B'
)

type enum_t uint32

// Device command definitions
var (
	BDR_CMD_GET_TARGET_INFO = _IOR(BDR_MAGIC, 1, unsafe.Sizeof(TargetInfo{}))

	enum_helper        enum_t = 0
	BDR_CMD_GET_STATUS        = _IOR(BDR_MAGIC, 2, unsafe.Sizeof(enum_helper))

	/* wait calls wait while no new writes available */
	BDR_CMD_GET_BUFFER_INFO       = _IOR(BDR_MAGIC, 3, unsafe.Sizeof(BufferInfo{}))
	BDR_CMD_GET_BUFFER_INFO_WAIT  = _IOR(BDR_MAGIC, 4, unsafe.Sizeof(BufferInfo{}))
	BDR_CMD_READ_BUFFER_INFO      = _IOR(BDR_MAGIC, 5, unsafe.Sizeof(BufferInfo{}))
	BDR_CMD_READ_BUFFER_INFO_WAIT = _IOR(BDR_MAGIC, 6, unsafe.Sizeof(BufferInfo{}))

	BDR_CMD_RESET_BUFFER = _IO(BDR_MAGIC, 7)
)

var ioctlCmdNames = map[uintptr]string{
	BDR_CMD_GET_TARGET_INFO:       "GET_TARGET_INFO",
	BDR_CMD_GET_STATUS:            "GET_STATUS",
	BDR_CMD_GET_BUFFER_INFO:       "GET_BUFFER_INFO",
	BDR_CMD_GET_BUFFER_INFO_WAIT:  "GET_BUFFER_INFO_WAIT",
	BDR_CMD_READ_BUFFER_INFO:      "READ_BUFFER_INFO",
	BDR_CMD_READ_BUFFER_INFO_WAIT: "READ_BUFFER_INFO_WAIT",
	BDR_CMD_RESET_BUFFER:          "RESET_BUFFER",
}

// Function to get the name of a command
func GetIOCTLCommandName(cmd uintptr) string {
	if name, ok := ioctlCmdNames[cmd]; ok {
		return name
	}
	return fmt.Sprintf("Unknown command (0x%X)", cmd)
}

// ioctl wrapper function
func ioctl(fd, request, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, arg)
	if errno != 0 {
		return errno
	}
	return nil
}
