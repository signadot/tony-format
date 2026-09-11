package issuelib

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/binary"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// XID is a 12-byte globally unique identifier.
// Structure:
//   - 4 bytes: Unix timestamp (seconds, big-endian)
//   - 3 bytes: machine ID
//   - 2 bytes: process ID
//   - 3 bytes: counter
//
// XIDR is the reversed form used for storage and display.
// Reversing puts counter/pid/machine first, making short prefixes unique.
type XID [12]byte

var (
	// Crockford's base32 alphabet (excludes I, L, O, U)
	base32Alphabet = "0123456789abcdefghjkmnpqrstvwxyz"

	machineID [3]byte
	pid       uint16
	counter   uint32
)

func init() {
	// Initialize machine ID from hostname
	hostname, err := os.Hostname()
	if err != nil {
		// Fallback to random
		rand.Read(machineID[:])
	} else {
		h := md5.Sum([]byte(hostname))
		copy(machineID[:], h[:3])
	}

	// Initialize PID
	pid = uint16(os.Getpid())

	// Initialize counter with random value
	var b [4]byte
	rand.Read(b[:])
	counter = binary.BigEndian.Uint32(b[:])
}

// NewXID creates an XID stamped with t. The counter advances atomically, so
// concurrent calls within the same second still differ, and it starts from a
// random value so two processes that begin at once do not walk the same
// sequence. Safe for concurrent use.
func NewXID(t time.Time) XID {
	var x XID

	// Timestamp (4 bytes, big-endian)
	ts := uint32(t.Unix())
	binary.BigEndian.PutUint32(x[0:4], ts)

	// Machine ID (3 bytes)
	x[4] = machineID[0]
	x[5] = machineID[1]
	x[6] = machineID[2]

	// PID (2 bytes, big-endian)
	x[7] = byte(pid >> 8)
	x[8] = byte(pid)

	// Counter (3 bytes, big-endian)
	c := atomic.AddUint32(&counter, 1)
	x[9] = byte(c >> 16)
	x[10] = byte(c >> 8)
	x[11] = byte(c)

	return x
}

// String returns the base32-encoded XID (20 characters).
func (x XID) String() string {
	return encodeXID(x[:])
}

// XIDR returns the XID in reversed form (XIDR), base32-encoded.
// This puts the counter/pid/machine bytes first, making short prefixes unique.
func (x XID) XIDR() string {
	var rev [12]byte
	for i := 0; i < 12; i++ {
		rev[i] = x[11-i]
	}
	return encodeXID(rev[:])
}

// encodeXID converts 12 bytes to 20-character base32 string.
// Uses 5-bit groups: 12 bytes = 96 bits = 19.2 groups, padded to 20.
func encodeXID(b []byte) string {
	var s [20]byte

	s[0] = base32Alphabet[b[0]>>3]
	s[1] = base32Alphabet[(b[0]&0x07)<<2|b[1]>>6]
	s[2] = base32Alphabet[(b[1]&0x3F)>>1]
	s[3] = base32Alphabet[(b[1]&0x01)<<4|b[2]>>4]
	s[4] = base32Alphabet[(b[2]&0x0F)<<1|b[3]>>7]
	s[5] = base32Alphabet[(b[3]&0x7F)>>2]
	s[6] = base32Alphabet[(b[4]>>5)|((b[3]&0x03)<<3)]
	s[7] = base32Alphabet[b[4]&0x1F]
	s[8] = base32Alphabet[b[5]>>3]
	s[9] = base32Alphabet[(b[5]&0x07)<<2|b[6]>>6]
	s[10] = base32Alphabet[(b[6]&0x3F)>>1]
	s[11] = base32Alphabet[(b[6]&0x01)<<4|b[7]>>4]
	s[12] = base32Alphabet[(b[7]&0x0F)<<1|b[8]>>7]
	s[13] = base32Alphabet[(b[8]&0x7F)>>2]
	s[14] = base32Alphabet[(b[9]>>5)|((b[8]&0x03)<<3)]
	s[15] = base32Alphabet[b[9]&0x1F]
	s[16] = base32Alphabet[b[10]>>3]
	s[17] = base32Alphabet[(b[10]&0x07)<<2|b[11]>>6]
	s[18] = base32Alphabet[(b[11]&0x3F)>>1]
	s[19] = base32Alphabet[(b[11]&0x01)<<4]

	return string(s[:])
}

// MatchesXIDRPrefix returns true if the XIDR starts with the given prefix.
// Case-insensitive.
func MatchesXIDRPrefix(prefix, xidr string) bool {
	if len(prefix) > len(xidr) {
		return false
	}
	return strings.EqualFold(prefix, xidr[:len(prefix)])
}
