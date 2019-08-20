package cypress

import (
	"encoding/gob"
	"errors"
	"io/ioutil"
	"os"
	"path"
	"time"

	"go.uber.org/zap"
)

var (
	// ErrDirectoryRequired a directory is expected but file is seen
	ErrDirectoryRequired = errors.New("a directory is required")

	// ErrFileRequired a file is expected but directory is seen
	ErrFileRequired = errors.New("a file is required")

	// ErrBadSessionFile failed to read session data from session file
	ErrBadSessionFile = errors.New("bad session file")
)

type fileSessionStore struct {
	path     string
	gcTicker *time.Ticker
	exitChan chan bool
}

type fileSessionItem struct {
	Data       []byte
	Expiration time.Time
}

// NewFileSessionStore creates a file session store
func NewFileSessionStore(directory string) (SessionStore, error) {
	fileInfo, err := os.Stat(directory)
	if err != nil {
		return nil, err
	}

	if !fileInfo.IsDir() {
		return nil, ErrDirectoryRequired
	}

	store := &fileSessionStore{
		path:     directory,
		gcTicker: time.NewTicker(time.Minute * 5),
		exitChan: make(chan bool),
	}

	gob.Register(fileSessionItem{})
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

	return store, nil
}

// Close close the session store
func (store *fileSessionStore) Close() {
	store.exitChan <- true
	store.gcTicker.Stop()
	close(store.exitChan)
}

// Save saves the session to the file system
func (store *fileSessionStore) Save(session *Session, timeout time.Duration) error {
	filePath := path.Join(store.path, session.ID)
	if !session.IsValid {
		// remove the session as it's set to invalid
		err := os.Remove(filePath)
		if err != nil {
			zap.L().Error("failed to remove session file", zap.Error(err))
		}

		return nil
	}

	// this acquires session's read lock, must be put before session.lock.Lock()
	sessionData := session.Serialize()

	// we need to lock the session to avoid concurrent writes
	session.lock.Lock()
	defer session.lock.Unlock()

	// try to remove the old session file if the file exists
	os.Remove(filePath)
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	defer file.Close()
	sessionItem := &fileSessionItem{
		Data:       sessionData,
		Expiration: time.Now().Add(timeout),
	}

	encoder := gob.NewEncoder(file)
	err = encoder.Encode(sessionItem)
	return err
}

// Get gets a session with the given id from the file store
func (store *fileSessionStore) Get(id string) (*Session, error) {
	sessionItem, err := store.readSession(id)
	if err != nil {
		return nil, err
	}

	if sessionItem == nil {
		return nil, ErrSessionNotFound
	}

	if sessionItem.Expiration.Before(time.Now()) {
		return nil, ErrSessionNotFound
	}

	session := NewSession(id)
	session.Deserialize(sessionItem.Data)
	return session, nil
}

func (store *fileSessionStore) readSession(id string) (*fileSessionItem, error) {
	filePath := path.Join(store.path, id)
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, ErrSessionNotFound
	}

	if fileInfo.IsDir() {
		return nil, ErrFileRequired
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	defer file.Close()
	decoder := gob.NewDecoder(file)
	sessionItem := fileSessionItem{}
	err = decoder.Decode(&sessionItem)
	if err != nil {
		return nil, err
	}

	return &sessionItem, nil
}

func (store *fileSessionStore) doGC() {
	files, err := ioutil.ReadDir(store.path)
	if err != nil {
		zap.L().Error("failed to run GC on file session store", zap.Error(err))
		return
	}

	now := time.Now()
	for _, file := range files {
		item, _ := store.readSession(file.Name())
		if item != nil && item.Expiration.Before(now) {
			os.Remove(path.Join(store.path, file.Name()))
			zap.L().Debug("session file expired, clean up by GC", zap.Error(err))
		}
	}
}
