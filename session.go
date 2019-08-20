package cypress

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/gob"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gofrs/uuid"
	"go.uber.org/zap"
)

const (
	sessionIDCookieKey = "_CYPRESS_SESS_ID"
)

var (
	// ErrSessionNotFound a session with the specified key cannot be found
	ErrSessionNotFound = errors.New("session not found")
)

// NewSessionID creates a new session ID
func NewSessionID() string {
	guid, err := uuid.NewV1()
	if err != nil {
		io.ReadFull(rand.Reader, guid[:])
	}

	return base64.RawURLEncoding.EncodeToString(guid[:])
}

// Session a HTTP session
type Session struct {
	ID      string
	IsValid bool
	isDirty bool
	data    map[string]interface{}
	lock    *sync.RWMutex
}

// NewSession creates a new session
func NewSession(id string) *Session {
	return &Session{
		ID:      id,
		IsValid: true,
		isDirty: false,
		data:    make(map[string]interface{}),
		lock:    new(sync.RWMutex),
	}
}

// SetValue sets a value to the session, returns the old value if the key has a value before
func (session *Session) SetValue(key string, value interface{}) (interface{}, bool) {
	session.lock.Lock()
	defer session.lock.Unlock()
	oldValue, ok := session.data[key]
	session.data[key] = value
	session.isDirty = true
	return oldValue, ok
}

// GetValue gets the value which is associated with the key, returns the value and key
// existing indicator
func (session *Session) GetValue(key string) (interface{}, bool) {
	session.lock.RLock()
	defer session.lock.RUnlock()
	value, ok := session.data[key]
	return value, ok
}

// GetAsFlashValue gets the value that is associated with the key and remove the value
// if the key existing, this returns the value  and the key existing indicator
func (session *Session) GetAsFlashValue(key string) (interface{}, bool) {
	session.lock.Lock()
	defer session.lock.Unlock()
	value, ok := session.data[key]
	if ok {
		delete(session.data, key)
	}

	session.isDirty = true
	return value, ok
}

// Serialize serializes the session data into bytes
func (session *Session) Serialize() []byte {
	session.lock.RLock()
	defer session.lock.RUnlock()
	buf := new(bytes.Buffer)
	encoder := gob.NewEncoder(buf)
	err := encoder.Encode(session.data)
	if err != nil {
		panic(err)
	}

	return buf.Bytes()
}

// Deserialize de-serializes the bytes into session data
func (session *Session) Deserialize(data []byte) {
	session.lock.Lock()
	defer session.lock.Unlock()
	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)
	err := decoder.Decode(&(session.data))
	if err != nil {
		panic(err)
	}
}

// NeedSave checks if the session needs to be saved back to store or not
func (session *Session) NeedSave() bool {
	return session.isDirty
}

// GetSession helper function for getting session from request
func GetSession(request *http.Request) *Session {
	value := request.Context().Value(SessionKey)
	if value != nil {
		return value.(*Session)
	}

	return nil
}

// SessionStore interface for storing sessions
type SessionStore interface {
	// Save saves the session with timeout, the session cannot be
	// returned by Get after the give timeout duration passed, while the
	// timeout need to be reset at each time the session is saved.
	// if the session's IsValid is set to false, after saved, the
	// session should never be returned by Get.
	Save(session *Session, timeout time.Duration) error

	// Get gets the session with the given ID, returns the session
	// if the session is still valid, otherwise, (nil, ErrSessionNotFound)
	// will be returned
	Get(id string) (*Session, error)

	// Close closes the session store and release the resources that
	// the session store owns
	Close()
}

type sessionHandler struct {
	store    SessionStore
	pipeline http.Handler
	timeout  time.Duration
}

// ServHTTP serves incoming http request
func (handler *sessionHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	var session *Session
	cookie, err := request.Cookie(sessionIDCookieKey)
	if err == nil {
		session, err = handler.store.Get(cookie.Value)
		if err != nil && err != ErrSessionNotFound {
			zap.L().Error("Not able to get session from session store", zap.Error(err), zap.String("activityId", GetTraceID(request.Context())))
			SendError(writer, http.StatusInternalServerError, "Session store failure")
			return
		}
	}

	if session == nil {
		session = NewSession(NewSessionID())
		cookie := &http.Cookie{
			Name:   sessionIDCookieKey,
			Value:  session.ID,
			MaxAge: 60 * 60 * 24, // for one day
			Path:   "/",
		}

		http.SetCookie(writer, cookie)
	}

	request.Context().(*multiValueCtx).withValue(SessionKey, session)
	defer func() {
		if session.NeedSave() {
			saveError := handler.store.Save(session, handler.timeout)
			if saveError != nil {
				zap.L().Error("Not able to save the session", zap.Error(err), zap.String("activityId", GetTraceID(request.Context())))
			}
		}
	}()

	handler.pipeline.ServeHTTP(writer, request)
}

// NewSessionHandler creates a new session handler with registering the session object types
func NewSessionHandler(pipeline http.Handler, store SessionStore, timeout time.Duration, valueTypes ...interface{}) http.Handler {
	gob.Register(make(map[string]interface{}))
	if valueTypes != nil {
		for _, valueType := range valueTypes {
			gob.Register(valueType)
		}
	}

	return &sessionHandler{store, pipeline, timeout}
}
