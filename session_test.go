package cypress

import (
	"encoding/gob"
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"github.com/go-redis/redis"
)

type TestObj struct {
	ID   int32
	Name string
}

func TestInMemorySessionStore(t *testing.T) {
	sessionStore := NewInMemorySessionStore()
	defer sessionStore.Close()
	testSessionStore(sessionStore, t)
}

func TestFileSessionStore(t *testing.T) {
	// setup the container
	container := NewSessionID()
	testDir := path.Join(os.TempDir(), container)
	gob.Register(TestObj{})
	err := os.Mkdir(testDir, os.ModePerm)
	if err != nil {
		t.Error("failed to create test folder", err)
		return
	}

	sessionStore, err := NewFileSessionStore(testDir)
	if err != nil {
		t.Error("failed to create file session store", err)
		return
	}

	testSessionStore(sessionStore, t)

	// clean up
	files, err := ioutil.ReadDir(testDir)
	if err != nil {
		t.Error("failed to list test folder")
		return
	}

	for _, file := range files {
		os.Remove(path.Join(testDir, file.Name()))
	}

	os.Remove(testDir)
}

// TestRedisSessionStore test redis store, enable this by change first character to upper case
// however, please make sure redis server is started without any password and default port before
// you run the test case
func testRedisSessionStore(t *testing.T) {
	gob.Register(TestObj{})
	redisdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	store := NewRedisSessionStore(redisdb)
	testSessionStore(store, t)
}

func testSessionStore(sessionStore SessionStore, t *testing.T) {
	session := NewSession(NewSessionID())
	session.SetValue("key1", "value1")
	session.SetValue("key2", &TestObj{123, "abc"})
	if !session.NeedSave() {
		t.Error("dirty flag is not set")
		return
	}

	err := sessionStore.Save(session, time.Millisecond*50)
	if err != nil {
		t.Error("failed to save session", err)
		return
	}

	session1, err := sessionStore.Get(session.ID)
	if err != nil {
		t.Error("session must exist")
		return
	}

	if session1.NeedSave() {
		t.Error("dirty flag is set")
		return
	}

	_, ok := session1.GetValue("key1")
	if !ok {
		t.Error("key1 must exist")
		return
	}

	time.Sleep(time.Millisecond * 51)
	_, err = sessionStore.Get(session.ID)
	if err != ErrSessionNotFound {
		t.Error("session must not be returned")
		return
	}

	session.ID = NewSessionID()
	sessionStore.Save(session, time.Minute)
	session.IsValid = false
	sessionStore.Save(session, time.Minute)
	_, err = sessionStore.Get(session.ID)
	if err != ErrSessionNotFound {
		t.Error("session must not be returned")
		return
	}
}
