package vulnfixtures

import (
	"io"
	"net/http"
)

// FetchURL downloads whatever URL the caller provides.
//
// [VULN-06] SSRF / unrestricted URL fetch: no allowlist, no scheme check, no
// timeout, so http://169.254.169.254/... (cloud metadata) can be reached.
// Scanner expectation: Semgrep "go.lang.security.audit.net.ssrf" /
// gosec (no timeout -> G107 in some versions).
func FetchURL(rawURL string) ([]byte, error) {
	resp, err := http.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
