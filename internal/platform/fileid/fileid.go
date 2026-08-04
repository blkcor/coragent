// Package fileid projects a filesystem object's stable platform identity
// without retaining an open descriptor. Unix Stat_t values expose device and
// inode through reflection; other platforms report an explicit unavailable
// marker and continue to rely on canonical-path identity.
package fileid

import (
	"fmt"
	"io/fs"
	"reflect"
	"runtime"
)

func FromInfo(info fs.FileInfo) string {
	if info == nil || info.Sys() == nil {
		return runtime.GOOS + ":unavailable"
	}
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return runtime.GOOS + ":unavailable"
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return runtime.GOOS + ":unavailable"
	}
	device := value.FieldByName("Dev")
	inode := value.FieldByName("Ino")
	deviceValue, deviceOK := unsignedValue(device)
	inodeValue, inodeOK := unsignedValue(inode)
	if !deviceOK || !inodeOK {
		return runtime.GOOS + ":unavailable"
	}
	if seconds, nanoseconds, ok := birthTime(value); ok {
		return fmt.Sprintf("%s:%d:%d:%d:%d", runtime.GOOS, deviceValue, inodeValue, seconds, nanoseconds)
	}
	return fmt.Sprintf("%s:%d:%d", runtime.GOOS, deviceValue, inodeValue)
}

func unsignedValue(value reflect.Value) (uint64, bool) {
	if !value.IsValid() {
		return 0, false
	}
	switch value.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		signed := value.Int()
		if signed < 0 {
			return 0, false
		}
		return uint64(signed), true
	default:
		return 0, false
	}
}

func HasBirthTime(info fs.FileInfo) bool {
	if info == nil || info.Sys() == nil {
		return false
	}
	value := reflect.ValueOf(info.Sys())
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return false
		}
		value = value.Elem()
	}
	_, _, ok := birthTime(value)
	return ok
}

func birthTime(value reflect.Value) (int64, int64, bool) {
	if value.Kind() != reflect.Struct {
		return 0, 0, false
	}
	birth := value.FieldByName("Birthtimespec")
	if !birth.IsValid() {
		return 0, 0, false
	}
	if birth.Kind() == reflect.Pointer {
		if birth.IsNil() {
			return 0, 0, false
		}
		birth = birth.Elem()
	}
	if birth.Kind() != reflect.Struct {
		return 0, 0, false
	}
	seconds := birth.FieldByName("Sec")
	nanoseconds := birth.FieldByName("Nsec")
	if !seconds.IsValid() || !nanoseconds.IsValid() || !seconds.CanInt() || !nanoseconds.CanInt() {
		return 0, 0, false
	}
	return seconds.Int(), nanoseconds.Int(), true
}
