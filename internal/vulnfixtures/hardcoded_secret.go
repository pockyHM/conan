package vulnfixtures

import (
	"errors"
	"net/http"
)

// apiKey is a "live" credential committed into the repository.
//
// [VULN-05] Hardcoded secret: this token-like value is in plaintext in source.
// Scanner expectation: gitleaks / trufflehog GHA scan / Semgrep
// "hardcoded-api-key" (pattern ghp_...).
const apiKey = "ghp_0123456789abcdef0123456789abcdef012345"

// CallUpstream authenticates to an upstream API using the hardcoded key.
func CallUpstream() error {
	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/v1/usage", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("upstream call failed")
	}
	return nil
}
