package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/restic/restic/internal/crypto"
	"github.com/restic/restic/internal/debug"
	"github.com/restic/restic/internal/restic"
)

type kdfCacheEntry struct {
	KeyID  string `json:"key_id"`
	N      int    `json:"N"`
	R      int    `json:"R"`
	P      int    `json:"P"`
	Salt   []byte `json:"salt"`
	Master []byte `json:"master"`
}

func kdfCacheFile(keyID restic.ID, password string) (string, error) {
	repoPath := fastModeRepoPath()
	if repoPath == "" {
		return "", nil
	}

	sum := sha256.Sum256([]byte(repoPath + "\x00" + password))
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(base, "restic-fast-kdf", hex.EncodeToString(sum[:16]))
	return filepath.Join(dir, keyID.String()+".json"), nil
}

func loadKDFCache(keyID restic.ID, password string, meta *Key) (*crypto.Key, bool) {
	if !fastModeEnabled() {
		return nil, false
	}

	path, err := kdfCacheFile(keyID, password)
	if err != nil || path == "" {
		return nil, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var entry kdfCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}

	if entry.KeyID != keyID.String() ||
		entry.N != meta.N || entry.R != meta.R || entry.P != meta.P ||
		len(entry.Salt) != len(meta.Salt) {
		return nil, false
	}
	for i := range entry.Salt {
		if entry.Salt[i] != meta.Salt[i] {
			return nil, false
		}
	}

	master := &crypto.Key{}
	if err := json.Unmarshal(entry.Master, master); err != nil {
		return nil, false
	}

	debug.Log("fast mode: KDF cache hit for key %v", keyID)
	return master, true
}

func saveKDFCache(keyID restic.ID, password string, meta *Key, master *crypto.Key) {
	if !fastModeEnabled() || master == nil {
		return
	}

	path, err := kdfCacheFile(keyID, password)
	if err != nil || path == "" {
		return
	}

	masterJSON, err := json.Marshal(master)
	if err != nil {
		return
	}

	entry := kdfCacheEntry{
		KeyID:  keyID.String(),
		N:      meta.N,
		R:      meta.R,
		P:      meta.P,
		Salt:   append([]byte(nil), meta.Salt...),
		Master: masterJSON,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return
	}
	debug.Log("fast mode: KDF cache saved for key %v", keyID)
}
