package security

import "testing"

func TestWhitelistMatchExact(t *testing.T) {
	w := NewWhitelist([]string{"cat", "ls", "free"})
	if !w.Match("cat") {
		t.Fatal("exact match should pass")
	}
	if !w.Match("free") {
		t.Fatal("exact match should pass")
	}
}

func TestWhitelistMatchPrefix(t *testing.T) {
	w := NewWhitelist([]string{"kubectl get", "ps aux", "docker ps"})
	if !w.Match("kubectl get pods -n default") {
		t.Fatal("prefix match should pass")
	}
	if !w.Match("ps aux") {
		t.Fatal("exact match of prefix entry should pass")
	}
	if !w.Match("docker ps --filter name=nginx") {
		t.Fatal("prefix match should pass")
	}
}

func TestWhitelistNoMatch(t *testing.T) {
	w := NewWhitelist([]string{"cat", "ls"})
	if w.Match("rm -rf /") {
		t.Fatal("should not match")
	}
	if w.Match("kubectl delete pod nginx") {
		t.Fatal("prefix should not match — whitelist has no kubectl delete")
	}
}

func TestWhitelistEmpty(t *testing.T) {
	w := NewWhitelist(nil)
	if w.Match("anything") {
		t.Fatal("empty whitelist should not match")
	}
}

func TestWhitelistTrimSpace(t *testing.T) {
	w := NewWhitelist([]string{"  cat  ", " ls "})
	if !w.Match("cat /etc/hosts") {
		t.Fatal("trimmed entry should match")
	}
	if !w.Match("ls -la") {
		t.Fatal("trimmed entry should match")
	}
}

func TestWhitelistCaseSensitive(t *testing.T) {
	w := NewWhitelist([]string{"cat"})
	if w.Match("CAT") {
		t.Fatal("matching should be case-sensitive")
	}
}
