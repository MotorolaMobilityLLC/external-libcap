// +build linux,allthreadssyscall,!cgo

package cap

import "syscall"

// multisc provides syscalls overridable for testing purposes that
// support a single kernel security state for all OS threads.
var multisc = &syscaller{
	w3: syscall.AllThreadsSyscall,
	w6: syscall.AllThreadsSyscall6,
	r3: syscall.RawSyscall,
	r6: syscall.RawSyscall6,
}

// singlesc provides a single threaded implementation. Users should
// take care to ensure the thread is locked and marked nogc.
var singlesc = &syscaller{
	w3: syscall.RawSyscall,
	w6: syscall.RawSyscall6,
	r3: syscall.RawSyscall,
	r6: syscall.RawSyscall6,
}
