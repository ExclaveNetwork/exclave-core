package singbridge

import (
	"reflect"
	"syscall"
	"testing"
)

func TestMethodImplementation(t *testing.T) {
	var conn *noSyscallConn
	if reflect.TypeOf(conn).Implements(reflect.TypeFor[syscall.Conn]()) {
		t.Error("noSyscallConn must not implement syscall.Conn")
	}
}
