// Package cap is the Linux capabilities user space API (libcap)
// bindings in native Go.
//
// For cgo linked binaries, package "libcap/psx" is used to broker the
// POSIX semantics system calls that manipulate process state.
//
// If the Go runtime syscall interface contains the
// syscall.PerOSThreadSyscall() API then then this package will use
// that to invoke capability setting system calls for pure Go
// binaries. To force this behavior use the CGO_ENABLED=0 environment
// variable.
//
// If syscall.PerOSThreadSyscall() is not present, the "libcap/cap"
// package will failover to using "libcap/psx".
package cap

import (
	"errors"
	"sort"
	"sync"
	"syscall"
	"unsafe"
)

// Value is the type of a single capability (or permission) bit.
type Value uint

// Flag is the type of one of the three Value vectors held in a Set.
type Flag uint

// Effective, Permitted, Inheritable are the three vectors of Values
// held in a Set.
const (
	Effective Flag = iota
	Permitted
	Inheritable
)

// data holds a 32-bit slice of the compressed bitmaps of capability
// sets as understood by the kernel.
type data [Inheritable + 1]uint32

// Set is an opaque capabilities container for a set of system
// capbilities.
type Set struct {
	// mu protects all other members of a Set.
	mu sync.RWMutex

	// flat holds Flag Value bitmaps for all capabilities
	// associated with this Set.
	flat []data

	// Linux specific
	nsRoot int
}

// Various known kernel magic values.
const (
	kv1 = 0x19980330 // First iteration of process capabilities (32 bits).
	kv2 = 0x20071026 // First iteration of process and file capabilities (64 bits) - deprecated.
	kv3 = 0x20080522 // Most recently supported process and file capabilities (64 bits).
)

var (
	// starUp protects setting of the following values: magic,
	// words, maxValues.
	startUp sync.Once

	// magic holds the preferred magic number for the kernel ABI.
	magic uint32

	// words holds the number of uint32's associated with each
	// capability vector for this session.
	words int

	// maxValues holds the number of bit values that are named by
	// the running kernel. This is generally expected to match
	// ValueCount which is autogenerated at packaging time.
	maxValues uint
)

type header struct {
	magic uint32
	pid   int32
}

// caprcall provides a pointer etc wrapper for the system calls
// associated with getcap.
func caprcall(call uintptr, h *header, d []data) error {
	x := uintptr(0)
	if d != nil {
		x = uintptr(unsafe.Pointer(&d[0]))
	}
	_, _, err := callRKernel(call, uintptr(unsafe.Pointer(h)), x, 0)
	if err != 0 {
		return err
	}
	return nil
}

// capwcall provides a pointer etc wrapper for the system calls
// associated with setcap.
func capwcall(call uintptr, h *header, d []data) error {
	x := uintptr(0)
	if d != nil {
		x = uintptr(unsafe.Pointer(&d[0]))
	}
	_, _, err := callWKernel(call, uintptr(unsafe.Pointer(h)), x, 0)
	if err != 0 {
		return err
	}
	return nil
}

// prctlrcall provides a wrapper for the prctl systemcalls that only
// read kernel state. There is a limited number of arguments needed
// and the caller should use 0 for those not needed.
func prctlrcall(prVal, v1, v2 uintptr) (int, error) {
	r, _, err := callRKernel(syscall.SYS_PRCTL, prVal, v1, v2)
	if err != 0 {
		return int(r), err
	}
	return int(r), nil
}

// prctlrcall6 provides a wrapper for the prctl systemcalls that only
// read kernel state and require 6 arguments - ambient cap API, I'm
// looking at you. There is a limited number of arguments needed and
// the caller should use 0 for those not needed.
func prctlrcall6(prVal, v1, v2, v3, v4, v5 uintptr) (int, error) {
	r, _, err := callRKernel6(syscall.SYS_PRCTL, prVal, v1, v2, v3, v4, v5)
	if err != 0 {
		return int(r), err
	}
	return int(r), nil
}

// prctlwcall provides a wrapper for the prctl systemcalls that
// write/modify kernel state. Where available, these will use the
// POSIX semantics fixup system calls. There is a limited number of
// arguments needed and the caller should use 0 for those not needed.
func prctlwcall(prVal, v1, v2 uintptr) (int, error) {
	r, _, err := callWKernel(syscall.SYS_PRCTL, prVal, v1, v2)
	if err != 0 {
		return int(r), err
	}
	return int(r), nil
}

// prctlwcall6 provides a wrapper for the prctl systemcalls that
// write/modify kernel state and require 6 arguments - ambient cap
// API, I'm looking at you. (Where available, these will use the POSIX
// semantics fixup system calls). There is a limited number of
// arguments needed and the caller should use 0 for those not needed.
func prctlwcall6(prVal, v1, v2, v3, v4, v5 uintptr) (int, error) {
	r, _, err := callWKernel6(syscall.SYS_PRCTL, prVal, v1, v2, v3, v4, v5)
	if err != 0 {
		return int(r), err
	}
	return int(r), nil
}

// cInit perfoms the lazy identification of the capability vintage of
// the running system.
func cInit() {
	h := &header{
		magic: kv3,
	}
	caprcall(syscall.SYS_CAPGET, h, nil)
	magic = h.magic
	switch magic {
	case kv1:
		words = 1
	case kv2, kv3:
		words = 2
	default:
		// Fall back to a known good version.
		magic = kv3
		words = 2
	}
	// Use the bounding set to evaluate which capabilities exist.
	maxValues = uint(sort.Search(32*words, func(n int) bool {
		_, err := GetBound(Value(n))
		return err != nil
	}))
	if maxValues == 0 {
		// Fall back to using the largest value defined at build time.
		maxValues = NamedCount
	}
}

// NewSet returns an empty capability set.
func NewSet() *Set {
	startUp.Do(cInit)
	return &Set{
		flat: make([]data, words),
	}
}

// ErrBadSet indicates a nil pointer was used for a *Set, or the
// request of the Set is invalid in some way.
var ErrBadSet = errors.New("bad capability set")

// Dup returns a copy of the specified capability set.
func (c *Set) Dup() (*Set, error) {
	if c == nil || len(c.flat) == 0 {
		return nil, ErrBadSet
	}
	n := NewSet()
	c.mu.RLock()
	defer c.mu.RUnlock()
	copy(n.flat, c.flat)
	n.nsRoot = c.nsRoot
	return n, nil
}

// GetPID returns the capability set associated with the target process
// id; pid=0 is an alias for current.
func GetPID(pid int) (*Set, error) {
	v := NewSet()
	if err := caprcall(syscall.SYS_CAPGET, &header{magic: magic, pid: int32(pid)}, v.flat); err != nil {
		return nil, err
	}
	return v, nil
}

// GetProc returns the capability Set of the current process. If the
// kernel is unable to determine the Set associated with the current
// process, the function panic()s.
func GetProc() *Set {
	c, err := GetPID(0)
	if err != nil {
		panic(err)
	}
	return c
}

// SetProc attempts to write the capability Set to the current
// process. The kernel will perform permission checks and an error
// will be returned if the attempt fails.
func (c *Set) SetProc() error {
	if c == nil || len(c.flat) == 0 {
		return ErrBadSet
	}
	return capwcall(syscall.SYS_CAPSET, &header{magic: magic}, c.flat)
}

// defines from uapi/linux/prctl.h
const (
	PR_CAPBSET_READ = 23
	PR_CAPBSET_DROP = 24
)

// GetBound determines if a specific capability is currently part of
// the local bounding set. On systems where the bounding set Value is
// not present, this function returns an error.
func GetBound(val Value) (bool, error) {
	v, err := prctlrcall(PR_CAPBSET_READ, uintptr(val), 0)
	if err != nil {
		return false, err
	}
	return v > 0, nil
}

// DropBound attempts to suppress bounding set Values. The kernel will
// never allow a bounding set Value bit to be raised once successfully
// dropped. However, dropping requires the current process is
// sufficiently capable (usually via cap.SETPCAP being raised in the
// Effective flag vector). Note, the drops are performed in order and
// if one bounding value cannot be dropped, the function returns
// immediately with an error which may leave the system in an
// ill-defined state.
func DropBound(val ...Value) error {
	for _, v := range val {
		if _, err := prctlwcall(PR_CAPBSET_DROP, uintptr(v), 0); err != nil {
			return err
		}
	}
	return nil
}

// defines from uapi/linux/prctl.h
const (
	PR_CAP_AMBIENT = 47

	PR_CAP_AMBIENT_IS_SET    = 1
	PR_CAP_AMBIENT_RAISE     = 2
	PR_CAP_AMBIENT_LOWER     = 3
	PR_CAP_AMBIENT_CLEAR_ALL = 4
)

// GetAmbient determines if a specific capability is currently part of
// the local ambient set. On systems where the ambient set Value is
// not present, this function returns an error.
func GetAmbient(val Value) (bool, error) {
	r, err := prctlrcall6(PR_CAP_AMBIENT, PR_CAP_AMBIENT_IS_SET, uintptr(val), 0, 0, 0)
	return r > 0, err
}

// SetAmbient attempts to set a specific Value bit to the enable
// state. This function will return an error if insufficient
// permission is available to perform this task. The settings are
// performed in order and the function returns immediately an error is
// detected.
func SetAmbient(enable bool, val ...Value) error {
	dir := uintptr(PR_CAP_AMBIENT_LOWER)
	if enable {
		dir = PR_CAP_AMBIENT_RAISE
	}
	for _, v := range val {
		_, err := prctlwcall6(PR_CAP_AMBIENT, dir, uintptr(v), 0, 0, 0)
		if err != nil {
			return err
		}
	}
	return nil
}

// ResetAmbient attempts to ensure the Ambient set is fully
// cleared. It works by first reading the set and if it finds any bits
// raised it will attempt a reset. This is a workaround for situations
// where the Ambient API is locked.
func ResetAmbient() error {
	var v bool
	var err error

	for c := Value(0); !v; c++ {
		if v, err = GetAmbient(c); err != nil {
			// no non-zero values found.
			return nil
		}
	}
	_, err = prctlwcall6(PR_CAP_AMBIENT, PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0, 0)
	return err
}
