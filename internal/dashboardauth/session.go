package dashboardauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	sessionCookieName  = "__Host-hexroute_session"
	ceremonyCookieName = "__Host-hexroute_ceremony"
	sessionTTL         = 12 * time.Hour
	ceremonyTTL        = 5 * time.Minute
	maxSessions        = 128
	maxCeremonies      = 64
)

type ceremonyKind uint8

const (
	ceremonyLogin ceremonyKind = iota + 1
	ceremonyRegistration
)

type ceremonyState struct {
	kind      ceremonyKind
	username  string
	principal metadata.UUID
	data      webauthn.SessionData
	expiresAt time.Time
}

type loginState struct {
	principal metadata.UUID
	username  string
	expiresAt time.Time
}

type sessionManager struct {
	mu         sync.Mutex
	random     io.Reader
	now        func() time.Time
	ceremonies map[[32]byte]ceremonyState
	sessions   map[[32]byte]loginState
}

func newSessionManager(
	randomSource io.Reader,
	now func() time.Time,
) (*sessionManager, error) {
	if randomSource == nil {
		randomSource = rand.Reader
	}
	if now == nil {
		now = time.Now
	}
	return &sessionManager{
		random:     randomSource,
		now:        now,
		ceremonies: make(map[[32]byte]ceremonyState),
		sessions:   make(map[[32]byte]loginState),
	}, nil
}

func (manager *sessionManager) issueCeremony(
	response http.ResponseWriter,
	state ceremonyState,
) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	now := manager.now().UTC()
	manager.prune(now)
	if len(manager.ceremonies) >= maxCeremonies {
		return ErrAuthUnavailable
	}
	token, digest, err := manager.newToken()
	if err != nil {
		return ErrAuthUnavailable
	}
	state.expiresAt = now.Add(ceremonyTTL)
	manager.ceremonies[digest] = state
	http.SetCookie(response, secureCookie(
		ceremonyCookieName,
		token,
		state.expiresAt,
		ceremonyTTL,
	))
	return nil
}

func (manager *sessionManager) consumeCeremony(
	response http.ResponseWriter,
	request *http.Request,
	want ceremonyKind,
) (ceremonyState, error) {
	cookie, err := request.Cookie(ceremonyCookieName)
	if err != nil {
		return ceremonyState{}, ErrCeremonyExpired
	}
	digest := sha256.Sum256([]byte(cookie.Value))
	manager.mu.Lock()
	defer manager.mu.Unlock()
	now := manager.now().UTC()
	manager.prune(now)
	state, ok := manager.ceremonies[digest]
	delete(manager.ceremonies, digest)
	expireCookie(response, ceremonyCookieName)
	if !ok || state.kind != want || !state.expiresAt.After(now) {
		return ceremonyState{}, ErrCeremonyExpired
	}
	return state, nil
}

func (manager *sessionManager) issueSession(
	response http.ResponseWriter,
	user User,
) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	now := manager.now().UTC()
	manager.prune(now)
	if len(manager.sessions) >= maxSessions {
		return ErrAuthUnavailable
	}
	token, digest, err := manager.newToken()
	if err != nil {
		return ErrAuthUnavailable
	}
	state := loginState{
		principal: user.PrincipalID,
		username:  user.Username,
		expiresAt: now.Add(sessionTTL),
	}
	manager.sessions[digest] = state
	http.SetCookie(response, secureCookie(
		sessionCookieName,
		token,
		state.expiresAt,
		sessionTTL,
	))
	return nil
}

func (manager *sessionManager) authenticate(
	request *http.Request,
) (loginState, error) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil {
		return loginState{}, ErrSessionExpired
	}
	digest := sha256.Sum256([]byte(cookie.Value))
	manager.mu.Lock()
	defer manager.mu.Unlock()
	now := manager.now().UTC()
	manager.prune(now)
	state, ok := manager.sessions[digest]
	if !ok || !state.expiresAt.After(now) {
		return loginState{}, ErrSessionExpired
	}
	return state, nil
}

func (manager *sessionManager) logout(
	response http.ResponseWriter,
	request *http.Request,
) {
	if cookie, err := request.Cookie(sessionCookieName); err == nil {
		digest := sha256.Sum256([]byte(cookie.Value))
		manager.mu.Lock()
		delete(manager.sessions, digest)
		manager.mu.Unlock()
	}
	expireCookie(response, sessionCookieName)
}

func (manager *sessionManager) prune(now time.Time) {
	for digest, state := range manager.ceremonies {
		if !state.expiresAt.After(now) {
			delete(manager.ceremonies, digest)
		}
	}
	for digest, state := range manager.sessions {
		if !state.expiresAt.After(now) {
			delete(manager.sessions, digest)
		}
	}
}

func (manager *sessionManager) newToken() (string, [32]byte, error) {
	var raw [32]byte
	if _, err := io.ReadFull(manager.random, raw[:]); err != nil {
		return "", [32]byte{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	return token, sha256.Sum256([]byte(token)), nil
}

func secureCookie(
	name string,
	value string,
	expires time.Time,
	ttl time.Duration,
) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(ttl.Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}

func expireCookie(response http.ResponseWriter, name string) {
	http.SetCookie(response, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0).UTC(),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}
