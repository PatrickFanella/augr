// Package economicid defines deterministic identifiers shared by canonical
// economic facts. PostgreSQL migration 68 implements the same byte contract.
package economicid

import (
	"crypto/sha256"
	"strconv"

	"github.com/google/uuid"
)

const componentSeparator = byte(0x1f)

// DeterministicUUID hashes a domain followed by unambiguous, length-prefixed
// UTF-8 components and renders the first 16 SHA-256 bytes as a UUID.
func DeterministicUUID(domain string, components ...string) uuid.UUID {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(domain))
	for _, component := range components {
		_, _ = hasher.Write([]byte{componentSeparator})
		_, _ = hasher.Write([]byte(strconv.Itoa(len([]byte(component)))))
		_, _ = hasher.Write([]byte{':'})
		_, _ = hasher.Write([]byte(component))
	}
	sum := hasher.Sum(nil)
	var id uuid.UUID
	copy(id[:], sum[:len(id)])
	return id
}
