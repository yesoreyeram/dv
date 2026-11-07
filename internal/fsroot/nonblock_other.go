//go:build !unix

package fsroot

// nonBlock is not needed where FIFOs cannot block an open.
const nonBlock = 0
