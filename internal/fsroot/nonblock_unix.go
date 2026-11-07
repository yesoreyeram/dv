//go:build unix

package fsroot

import "syscall"

// nonBlock opens FIFOs without blocking so they can be rejected as non-regular files.
const nonBlock = syscall.O_NONBLOCK
