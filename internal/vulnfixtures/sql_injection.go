package vulnfixtures

import (
	"database/sql"
	"fmt"
)

// SearchMemories queries memory entries whose title contains q.
//
// [VULN-02] SQL injection: the query is assembled with string concatenation,
// so q = `' OR '1'='1' UNION SELECT secret FROM secrets --` leaks rows.
// Scanner expectation: gosec G201 / Semgrep "sql-injection".
func SearchMemories(db *sql.DB, q string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf(
		"SELECT content FROM memories WHERE title LIKE '%%%s%%'", q))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil {
			return nil, err
		}
		out = append(out, content)
	}
	return out, rows.Err()
}
