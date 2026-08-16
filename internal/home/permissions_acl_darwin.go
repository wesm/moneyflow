//go:build darwin

package home

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	attributeReferenceSize = 8
	fileSecurityACLCount   = 36
	noACL                  = ^uint32(0)
)

func rejectExtendedACLPath(path string) error {
	encoded, err := unix.BytePtrFromString(path)
	if err != nil {
		return fmt.Errorf("secure profile path: encode ACL path: %w", err)
	}
	hasACL, err := extendedACL(func(attributes *unix.Attrlist, buffer []byte) syscall.Errno {
		//nolint:gosec,staticcheck // Darwin exposes no pure-Go getattrlist wrapper.
		_, _, errno := syscall.Syscall6(
			unix.SYS_GETATTRLIST,
			uintptr(unsafe.Pointer(encoded)),
			uintptr(unsafe.Pointer(attributes)),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
			0,
			0,
		)
		return errno
	})
	if err != nil {
		return fmt.Errorf("secure profile path: inspect extended ACL: %w", err)
	}
	if hasACL {
		return errors.New("secure profile path: extended ACL is not permitted")
	}
	return nil
}

func rejectExtendedACLFile(file *os.File) error {
	hasACL, err := extendedACL(func(attributes *unix.Attrlist, buffer []byte) syscall.Errno {
		//nolint:gosec,staticcheck // Darwin exposes no pure-Go fgetattrlist wrapper.
		_, _, errno := syscall.Syscall6(
			unix.SYS_FGETATTRLIST,
			file.Fd(),
			uintptr(unsafe.Pointer(attributes)),
			uintptr(unsafe.Pointer(&buffer[0])),
			uintptr(len(buffer)),
			0,
			0,
		)
		return errno
	})
	if err != nil {
		return fmt.Errorf("read private file: inspect extended ACL: %w", err)
	}
	if hasACL {
		return errors.New("read private file: extended ACL is not permitted")
	}
	return nil
}

func extendedACL(call func(*unix.Attrlist, []byte) syscall.Errno) (bool, error) {
	attributes := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Commonattr: unix.ATTR_CMN_EXTENDED_SECURITY}
	buffer := make([]byte, 4096)
	if errno := call(&attributes, buffer); errno != 0 {
		return false, errno
	}
	if len(buffer) < 4+attributeReferenceSize {
		return false, errors.New("extended ACL response is truncated")
	}
	resultLength := int(binary.LittleEndian.Uint32(buffer[:4]))
	if resultLength < 4+attributeReferenceSize || resultLength > len(buffer) {
		return false, errors.New("extended ACL response length is invalid")
	}
	reference := 4
	rawOffset := binary.LittleEndian.Uint32(buffer[reference : reference+4])
	if rawOffset > math.MaxInt32 {
		return false, errors.New("extended ACL payload offset is invalid")
	}
	dataOffset := int(rawOffset)
	dataLength := int(binary.LittleEndian.Uint32(buffer[reference+4 : reference+8]))
	dataStart := reference + dataOffset
	if dataLength == 0 {
		return false, nil
	}
	if dataStart < 0 || dataLength < fileSecurityACLCount+4 ||
		dataStart+dataLength > resultLength {
		return false, errors.New("extended ACL payload is invalid")
	}
	entryCount := binary.LittleEndian.Uint32(
		buffer[dataStart+fileSecurityACLCount : dataStart+fileSecurityACLCount+4],
	)
	return entryCount != noACL && entryCount != 0, nil
}
