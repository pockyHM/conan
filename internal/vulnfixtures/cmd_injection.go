package vulnfixtures

import "os/exec"

// RunShellCommand echoes userInput through a shell.
//
// [VULN-01] OS command injection: user-controlled input is interpolated into
// "sh -c" without validation, so input like "$(rm -rf /tmp/x)" is executed.
// Scanner expectation: gosec G204 / Semgrep "shell-injection" / CodeQL
// "go-os-command-injection".
func RunShellCommand(userInput string) ([]byte, error) {
	return exec.Command("sh", "-c", "echo "+userInput).Output()
}
