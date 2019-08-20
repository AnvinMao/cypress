package cypress

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestMd5(t *testing.T) {
	s := "text to hash"
	result := hex.EncodeToString(Md5([]byte(s)))
	if result != "27703945b9bceacb09546d2e103ad360" {
		t.Error("27703945b9bceacb09546d2e103ad360 expected but", result)
	}
}

func TestSha1(t *testing.T) {
	s := "text to hash"
	result := hex.EncodeToString(Sha1([]byte(s)))
	if result != "1e0a5da7cf8d083e5d170db4e5cd03dc5b22d3fa" {
		t.Error(result, "but 1e0a5da7cf8d083e5d170db4e5cd03dc5b22d3fa expected")
	}
}

func TestSha256(t *testing.T) {
	s := "text to hash"
	result := hex.EncodeToString(Sha256([]byte(s)))
	if result != "119e3f0d28cf6a92d29399d5787f90308b6b87670d8c2386ec42cb36e293b5c4" {
		t.Error(result, "but 119e3f0d28cf6a92d29399d5787f90308b6b87670d8c2386ec42cb36e293b5c4 expected")
	}
}

func TestAes256Decrypt(t *testing.T) {
	s := "jnqPJ_spawkejMUW4FPizG4nqmL8OOjafPaMyDd6ge8"
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Error(s, "is not a valid base64 url encoded string")
		return
	}

	decrypted, err := Aes256Decrypt([]byte("weakpassword"), []byte("weakiv"), data)
	if err != nil {
		t.Error("failed to decrypt data")
		return
	}

	if "221.1001.1537075710000" != string(decrypted) {
		t.Error("221.1001.1537075710000 expected but got", string(decrypted))
	}
}

func TestAes256Encrypt(t *testing.T) {
	s := "221.1001.1537075710000"
	encrypted, err := Aes256Encrypt([]byte("weakpassword"), []byte("weakiv"), []byte(s))
	if err != nil {
		t.Error("failed to encrypt data")
		return
	}

	text := base64.RawURLEncoding.EncodeToString(encrypted)
	if "jnqPJ_spawkejMUW4FPizG4nqmL8OOjafPaMyDd6ge8" != text {
		t.Error("jnqPJ_spawkejMUW4FPizG4nqmL8OOjafPaMyDd6ge8 expected but got", text)
	}
}
