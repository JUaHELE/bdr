package main

import (
	"syscall"
	"unsafe"
)

func _IOR(t, nr, size uintptr) uintptr {
	return (_IOC_READ << _IOC_DIRSHIFT) | (t << _IOC_TYPESHIFT) | (nr << _IOC_NRSHIFT) | (size << _IOC_SIZESHIFT)
}

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

const (
	BDR_MAGIC = 'B'
)

var (
	BDR_CMD_GET_BUFFER_INFO = _IOR(BDR_MAGIC, 1, unsafe.Sizeof(BufferInfo{}))
	BDR_CMD_GET_TARGET_INFO = _IOR(BDR_MAGIC, 2, unsafe.Sizeof(TargetInfo{}))
)

func ioctl(fd, request, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, arg)
	if errno != 0 {
		return errno
	}
	return nil
}
