package cypress

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

type sessionItem struct {
	session    *Session
	expiration time.Time
}

type inMemorySessionStore struct {
	sessions map[string]*sessionItem
	lock     *sync.RWMutex
	gcTicker *time.Ticker
	exitChan chan bool
}

// NewInMemorySessionStore creates an in memory session store
func NewInMemorySessionStore() SessionStore {
	store := &inMemorySessionStore{
		sessions: make(map[string]*sessionItem),
		lock:     new(sync.RWMutex),
		gcTicker: time.NewTicker(5 * time.Minute),
		exitChan: make(chan bool),
	}

	go func() {
		for {
			select {
			case <-store.gcTicker.C:
				store.doGC()
				break
			case <-store.exitChan:
				return
			}
		}
	}()

	return store
}

// Close closes the session store
func (store *inMemorySessionStore) Close() {
	store.exitChan <- true
	store.gcTicker.Stop()
	close(store.exitChan)
}

// Save saves the session into store
func (store *inMemorySessionStore) Save(session *Session, timeout time.Duration) error {
	store.lock.Lock()
	defer store.lock.Unlock()
	if !session.IsValid {
		delete(store.sessions, session.ID)
	} else {
		item, ok := store.sessions[session.ID]
		if ok && item.session.IsValid && item.expiration.After(time.Now()) {
			item.expiration = time.Now().Add(timeout)
		} else {
			store.sessions[session.ID] = &sessionItem{
				session:    session,
				expiration: time.Now().Add(timeout),
			}
		}
	}

	return nil
}

// Get retrieves the session by session ID
func (store *inMemorySessionStore) Get(id string) (*Session, error) {
	store.lock.RLock()
	defer store.lock.RUnlock()
	item, ok := store.sessions[id]
	if !ok || item.expiration.Before(time.Now()) {
		return nil, ErrSessionNotFound
	}

	item.session.isDirty = false
	return item.session, nil
}

func (store *inMemorySessionStore) doGC() {
	keysToRemove := make([]string, 0)
	func() {
		store.lock.RLock()
		defer store.lock.RUnlock()
		now := time.Now()
		for key, value := range store.sessions {
			if value.expiration.Before(now) {
				keysToRemove = append(keysToRemove, key)
			}
		}
	}()

	store.lock.Lock()
	defer store.lock.Unlock()
	for _, key := range keysToRemove {
		delete(store.sessions, key)
		zap.L().Debug("session released by GC", zap.String("session", key))
	}
}
