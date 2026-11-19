// Copyright 2026 Flamego. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package altcha

import (
	"sync"
	"time"
)

// memoryStore is a ReplayStore that keeps used keys in memory until they
// expire.
type memoryStore struct {
	mu        sync.Mutex
	used      map[string]time.Time
	nextPurge time.Time
}

func newMemoryStore() *memoryStore {
	return &memoryStore{used: make(map[string]time.Time)}
}

func (s *memoryStore) Use(key string, expiresAt time.Time) (bool, error) {
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	if now.After(s.nextPurge) {
		for k, exp := range s.used {
			if !exp.After(now) {
				delete(s.used, k)
			}
		}
		s.nextPurge = now.Add(time.Minute)
	}

	if exp, ok := s.used[key]; ok && exp.After(now) {
		return false, nil
	}
	s.used[key] = expiresAt
	return true, nil
}
