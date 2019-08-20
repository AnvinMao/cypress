package cypress

import (
	"time"

	"github.com/go-redis/redis"
)

type redisSessionStore struct {
	redisDb *redis.Client
}

// NewRedisSessionStore creates a new redis based session store
func NewRedisSessionStore(cli *redis.Client) SessionStore {
	return &redisSessionStore{cli}
}

// Close closes the store
func (store *redisSessionStore) Close() {
	store.redisDb.Close()
}

// Save implements SessionStore's Save api, store the session data into redis
func (store *redisSessionStore) Save(session *Session, timeout time.Duration) error {
	if !session.IsValid {
		status := store.redisDb.Del(session.ID)
		return status.Err()
	}

	data := session.Serialize()
	status := store.redisDb.Set(session.ID, data, timeout)
	return status.Err()
}

// Get implements SessionStore's Get api, retrieves session from redis by given id
func (store *redisSessionStore) Get(id string) (*Session, error) {
	status := store.redisDb.Get(id)
	if status.Err() != nil {
		return nil, ErrSessionNotFound
	}

	data, err := status.Bytes()
	if err != nil {
		return nil, ErrSessionNotFound
	}

	session := NewSession(id)
	session.Deserialize(data)
	return session, nil
}
