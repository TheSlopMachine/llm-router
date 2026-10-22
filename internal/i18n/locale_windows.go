//go:build windows

package i18n

import (
	"syscall"
	"unsafe"
)

const localeNameLength = 85

// systemLocale reads the user locale name, for example "ru-RU".
func systemLocale() string {
	procedure := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")
	buffer := make([]uint16, localeNameLength)
	length, _, _ := procedure.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer)
}
