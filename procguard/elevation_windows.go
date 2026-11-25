//go:build windows

package procguard

import (
	"syscall"
	"unsafe"
)

const tokenElevation = 20 // TOKEN_INFORMATION_CLASS: TokenElevation

// Elevated reports whether the current process token is elevated.
// An error is returned together with true: when the answer cannot be read,
// refusing is the safe side, so a caller that ignores the error still refuses.
func Elevated() (bool, error) {
	proc, err := syscall.GetCurrentProcess()
	if err != nil {
		return true, err
	}
	var token syscall.Token
	if err := syscall.OpenProcessToken(proc, syscall.TOKEN_QUERY, &token); err != nil {
		return true, err
	}
	defer token.Close()
	var elevation uint32
	var returned uint32
	if err := syscall.GetTokenInformation(token, tokenElevation,
		(*byte)(unsafe.Pointer(&elevation)), uint32(unsafe.Sizeof(elevation)), &returned); err != nil {
		return true, err
	}
	return elevation != 0, nil
}
