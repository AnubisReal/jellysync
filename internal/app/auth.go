package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const sessionCookie = "jellysync_session"

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]time.Time)}
}

func hashPassword(password, saltHex string) string {
	salt, _ := hex.DecodeString(saltHex)
	result := pbkdf2SHA256([]byte(password), salt, 210000, 32)
	return hex.EncodeToString(result)
}

func pbkdf2SHA256(password, salt []byte, iterations, length int) []byte {
	hashLength := sha256.Size
	blocks := (length + hashLength - 1) / hashLength
	output := make([]byte, 0, blocks*hashLength)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		output = append(output, t...)
	}
	return output[:length]
}

func (s *sessionStore) create(w http.ResponseWriter, r *http.Request) {
	token := randomToken(32)
	expires := time.Now().Add(12 * time.Hour)
	s.mu.Lock()
	s.sessions[token] = expires
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires, MaxAge: 43200,
		HttpOnly: true, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *sessionStore) valid(r *http.Request) bool {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expires, ok := s.sessions[cookie.Value]
	if !ok || time.Now().After(expires) {
		delete(s.sessions, cookie.Value)
		return false
	}
	return true
}

func passwordMatches(password string, cfg Config) bool {
	if cfg.AdminHash == "" || cfg.AdminSalt == "" {
		return false
	}
	actual := hashPassword(password, cfg.AdminSalt)
	if len(actual) != len(cfg.AdminHash) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(cfg.AdminHash)) == 1
}
