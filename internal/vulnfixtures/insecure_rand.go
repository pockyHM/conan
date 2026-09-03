package vulnfixtures

import (
	"fmt"
	"math/rand"
)

// NewSessionToken returns a session token from math/rand.
//
// [VULN-07] Predictable random token: math/rand is not cryptographically
// secure, so tokens can be guessed. Scanner expectation: gosec G404.
func NewSessionToken() string {
	return fmt.Sprintf("tok-%d", rand.Int63())
}
