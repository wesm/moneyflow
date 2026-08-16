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
	fileSecurityACLHeader  = fileSecurityACLCount + 8
	fileSecurityACESize    = 24
	fileSecurityACEFlags   = 16
	noACL                  = ^uint32(0)
	kauthACEDeny           = uint32(2)
)

type extendedACLClass uint8

const (
	extendedACLAbsent extendedACLClass = iota
	extendedACLDenyOnly
	extendedACLPermissive
)

func rejectExtendedACLPath(path string) error {
	classification, err := classifyExtendedACLPath(path)
	if err != nil {
		return fmt.Errorf("secure profile path: inspect extended ACL: %w", err)
	}
	if classification != extendedACLAbsent {
		return errors.New("secure profile path: extended ACL is not permitted")
	}
	return nil
}

func rejectPermissiveExtendedACLPath(path string) error {
	classification, err := classifyExtendedACLPath(path)
	if err != nil {
		return fmt.Errorf("secure profile path: inspect extended ACL: %w", err)
	}
	if classification == extendedACLPermissive {
		return errors.New("secure profile path: permissive extended ACL is not permitted")
	}
	return nil
}

func classifyExtendedACLPath(path string) (extendedACLClass, error) {
	encoded, err := unix.BytePtrFromString(path)
	if err != nil {
		return extendedACLAbsent, fmt.Errorf("encode ACL path: %w", err)
	}
	return extendedACL(func(attributes *unix.Attrlist, buffer []byte) syscall.Errno {
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
}

func rejectExtendedACLFile(file *os.File) error {
	classification, err := extendedACL(func(attributes *unix.Attrlist, buffer []byte) syscall.Errno {
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
	if classification != extendedACLAbsent {
		return errors.New("read private file: extended ACL is not permitted")
	}
	return nil
}

func extendedACL(
	call func(*unix.Attrlist, []byte) syscall.Errno,
) (extendedACLClass, error) {
	attributes := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Commonattr: unix.ATTR_CMN_EXTENDED_SECURITY}
	buffer := make([]byte, 4096)
	if errno := call(&attributes, buffer); errno != 0 {
		return extendedACLAbsent, errno
	}
	if len(buffer) < 4+attributeReferenceSize {
		return extendedACLAbsent, errors.New("extended ACL response is truncated")
	}
	resultLength := int(binary.LittleEndian.Uint32(buffer[:4]))
	if resultLength < 4+attributeReferenceSize || resultLength > len(buffer) {
		return extendedACLAbsent, errors.New("extended ACL response length is invalid")
	}
	reference := 4
	rawOffset := binary.LittleEndian.Uint32(buffer[reference : reference+4])
	if rawOffset > math.MaxInt32 {
		return extendedACLAbsent, errors.New("extended ACL payload offset is invalid")
	}
	dataOffset := int(rawOffset)
	dataLength := int(binary.LittleEndian.Uint32(buffer[reference+4 : reference+8]))
	dataStart := reference + dataOffset
	if dataLength == 0 {
		return extendedACLAbsent, nil
	}
	if dataStart < 0 || dataLength < fileSecurityACLHeader ||
		dataStart+dataLength > resultLength {
		return extendedACLAbsent, errors.New("extended ACL payload is invalid")
	}
	entryCount := binary.LittleEndian.Uint32(
		buffer[dataStart+fileSecurityACLCount : dataStart+fileSecurityACLCount+4],
	)
	if entryCount == noACL || entryCount == 0 {
		return extendedACLAbsent, nil
	}
	entryBytes := dataLength - fileSecurityACLHeader
	entryCount64 := int64(entryCount)
	entryBytes64 := int64(entryBytes)
	if entryCount64 > entryBytes64/int64(fileSecurityACESize) ||
		entryCount64*int64(fileSecurityACESize) != entryBytes64 {
		return extendedACLAbsent, errors.New("extended ACL entries are invalid")
	}
	for index := range int(entryCount) {
		flagsStart := dataStart + fileSecurityACLHeader + index*fileSecurityACESize +
			fileSecurityACEFlags
		flags := binary.LittleEndian.Uint32(buffer[flagsStart : flagsStart+4])
		if flags&0xf != kauthACEDeny {
			return extendedACLPermissive, nil
		}
	}
	return extendedACLDenyOnly, nil
}
