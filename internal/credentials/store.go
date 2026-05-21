package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Credential struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Store struct {
	home string
}

func NewStore(home string) *Store {
	return &Store{home: home}
}

func (s *Store) Get(key string) (Credential, bool, error) {
	records, err := s.readRecords()
	if err != nil {
		return Credential{}, false, err
	}
	cred, ok := records[key]
	return cred, ok, nil
}

func (s *Store) Put(key string, cred Credential) error {
	records, err := s.readRecords()
	if err != nil {
		return err
	}
	records[key] = cred
	return s.writeRecords(records)
}

func (s *Store) readRecords() (map[string]Credential, error) {
	key, err := s.loadOrCreateKey()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.home, "credentials.enc")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Credential{}, nil
		}
		return nil, err
	}
	plain, err := decrypt(key, data)
	if err != nil {
		return nil, err
	}
	var records map[string]Credential
	if err := json.Unmarshal(plain, &records); err != nil {
		return nil, err
	}
	if records == nil {
		records = map[string]Credential{}
	}
	return records, nil
}

func (s *Store) writeRecords(records map[string]Credential) error {
	key, err := s.loadOrCreateKey()
	if err != nil {
		return err
	}
	plain, err := json.Marshal(records)
	if err != nil {
		return err
	}
	sealed, err := encrypt(key, plain)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.home, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.home, "credentials.enc"), sealed, 0600)
}

func (s *Store) loadOrCreateKey() ([]byte, error) {
	if err := os.MkdirAll(s.home, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(s.home, "credentials.key")
	key, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		key = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, key, 0600); err != nil {
			return nil, err
		}
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid credential key length: %d", len(key))
	}
	return key, nil
}

func encrypt(key []byte, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func decrypt(key []byte, sealed []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, fmt.Errorf("encrypted credentials file is too short")
	}
	nonce := sealed[:gcm.NonceSize()]
	ciphertext := sealed[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
