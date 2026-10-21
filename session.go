package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// Sealer seals small values into cookies (AES-256-GCM): the browser keeps the sign-in, the server keeps nothing.
type Sealer struct{ aead cipher.AEAD }

// NewSealer takes a 32-byte key as 64 hex characters.
func NewSealer(keyHex string) (*Sealer, error) {
	k, err := hex.DecodeString(keyHex)
	if err != nil || len(k) != 32 {
		return nil, errors.New("WAYFEED_COOKIE_KEY must be 64 hex characters (32 bytes)")
	}
	b, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	return &Sealer{aead: g}, err
}

// Seal encodes v as JSON and seals it.
func (s *Sealer) Seal(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(s.aead.Seal(nonce, nonce, raw, nil)), nil
}

// Open reverses Seal; a changed or foreign value is an error.
func (s *Sealer) Open(token string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) < s.aead.NonceSize() {
		return errors.New("bad cookie")
	}
	raw, err := s.aead.Open(nil, b[:s.aead.NonceSize()], b[s.aead.NonceSize():], nil)
	if err != nil {
		return errors.New("bad cookie")
	}
	return json.Unmarshal(raw, v)
}

// Session is what the sign-in cookie holds: the feedql tokens of the person.
type Session struct {
	Access  string    `json:"a"`
	Refresh string    `json:"r"`
	Expires time.Time `json:"e"`
}
