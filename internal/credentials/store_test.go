package credentials

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStorePutGetCreatesEncryptedFiles(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)

	cred := Credential{Username: "deploy", Password: "secret-password"}
	if err := store.Put("ssh/prod/web-1", cred); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok, err := store.Get("ssh/prod/web-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("credential missing")
	}
	if got != cred {
		t.Fatalf("credential = %+v, want %+v", got, cred)
	}

	data, err := os.ReadFile(filepath.Join(home, "credentials.enc"))
	if err != nil {
		t.Fatalf("read encrypted file: %v", err)
	}
	if strings.Contains(string(data), "deploy") || strings.Contains(string(data), "secret-password") {
		t.Fatalf("encrypted file contains plaintext: %q", data)
	}
}

func TestStoreMissingCredential(t *testing.T) {
	got, ok, err := NewStore(t.TempDir()).Get("ssh/prod/web-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatalf("ok = true, got %+v", got)
	}
}

func TestStoreFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are platform-specific on windows")
	}
	home := t.TempDir()
	store := NewStore(home)
	if err := store.Put("ssh/prod/web-1", Credential{Username: "deploy", Password: "secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	for _, name := range []string{"credentials.key", "credentials.enc"} {
		info, err := os.Stat(filepath.Join(home, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode = %o, want 0600", name, info.Mode().Perm())
		}
	}
}

func TestStoreCorruptEncryptedFileReturnsError(t *testing.T) {
	home := t.TempDir()
	store := NewStore(home)
	if err := store.Put("ssh/prod/web-1", Credential{Username: "deploy", Password: "secret"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "credentials.enc"), []byte("not-ciphertext"), 0600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	_, _, err := store.Get("ssh/prod/web-1")
	if err == nil {
		t.Fatal("expected corrupt file error")
	}
}
