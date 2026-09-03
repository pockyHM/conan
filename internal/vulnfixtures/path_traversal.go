package vulnfixtures

import (
	"net/http"
	"os"
)

// ServeReport streams the local file named by ?file= back to the client.
//
// [VULN-03] Path traversal / arbitrary file read: the query parameter is used
// directly as a filesystem path, so /reports?file=../../etc/passwd works.
// Scanner expectation: gosec G304 / Semgrep "path-join-resolve-traversal".
func ServeReport(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("file")
	data, err := os.ReadFile(name)
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(data)
}
