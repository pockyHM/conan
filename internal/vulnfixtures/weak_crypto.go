package vulnfixtures

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
)

// HashPassword returns the MD5 digest of password.
//
// [VULN-04] Weak hash for passwords: MD5 is trivially brute-forced and has no
// salt. Scanner expectation: gosec G501 / Semgrep "md5-used-as-password".
func HashPassword(password string) string {
	sum := md5.Sum([]byte(password))
	return hex.EncodeToString(sum[:])
}

// OldSignature computes a SHA1 MAC over message+key.
//
// [VULN-04b] SHA1 is cryptographically broken for MACs.
// Scanner expectation: gosec G505 / Semgrep "sha1".
func OldSignature(message, key string) string {
	h := sha1.New()
	_, _ = h.Write([]byte(message + key))
	return hex.EncodeToString(h.Sum(nil))
}
