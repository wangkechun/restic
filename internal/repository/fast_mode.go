package repository

import (
	"sync"

	"github.com/restic/restic/internal/crypto"
	"github.com/restic/restic/internal/restic"
)

var (
	fastModeMu    sync.RWMutex
	fastMode      bool
	fastCacheRepo string
)

// EnableFastMode turns on local fast paths: KDF cache and low-security params for new keys.
func EnableFastMode(repoPath string) {
	fastModeMu.Lock()
	defer fastModeMu.Unlock()
	fastMode = true
	fastCacheRepo = repoPath
	restic.EnableFastMode()
	paramsOnce.Do(func() {
		params = &crypto.Params{
			N: 128,
			R: 1,
			P: 1,
		}
	})
}

func fastModeEnabled() bool {
	fastModeMu.RLock()
	defer fastModeMu.RUnlock()
	return fastMode
}

func fastModeRepoPath() string {
	fastModeMu.RLock()
	defer fastModeMu.RUnlock()
	return fastCacheRepo
}
