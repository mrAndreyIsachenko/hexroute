package dashboardauth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/mrAndreyIsachenko/hexroute/internal/cutoverfreeze"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const maxAuthRequestBytes = 64 * 1024

type Store interface {
	LoadUser(context.Context, string) (User, error)
	AddCredential(context.Context, User, *webauthn.Credential, time.Time) error
	UpdateCredential(context.Context, User, *webauthn.Credential, time.Time) error
}

type Handler struct {
	store           Store
	ceremony        Ceremony
	sessions        *sessionManager
	origin          string
	bootstrapSecret []byte
	now             func() time.Time
	freeze          cutoverfreeze.Reader
}

type Config struct {
	Store           Store
	Ceremony        Ceremony
	Origin          string
	BootstrapSecret string
	Random          io.Reader
	Now             func() time.Time
	Freeze          cutoverfreeze.Reader
}

func NewHandler(config Config) (*Handler, error) {
	if config.Store == nil ||
		config.Ceremony == nil ||
		!validOrigin(config.Origin) ||
		len(config.BootstrapSecret) < 32 {
		return nil, ErrInvalidAuthConfig
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	sessions, err := newSessionManager(config.Random, config.Now)
	if err != nil {
		return nil, err
	}
	return &Handler{
		store:           config.Store,
		ceremony:        config.Ceremony,
		sessions:        sessions,
		origin:          config.Origin,
		bootstrapSecret: []byte(config.BootstrapSecret),
		now:             config.Now,
		freeze:          config.Freeze,
	}, nil
}

func (handler *Handler) BeginLogin(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !handler.acceptPOST(response, request) {
		return
	}
	username, ok := decodeUsername(response, request)
	if !ok {
		return
	}
	user, err := handler.store.LoadUser(request.Context(), username)
	if err != nil || len(user.Credentials) == 0 {
		writeAuthError(response, http.StatusUnauthorized)
		return
	}
	options, session, err := handler.ceremony.BeginLogin(user)
	if err != nil || session == nil {
		writeAuthError(response, http.StatusUnauthorized)
		return
	}
	if err := handler.sessions.issueCeremony(response, ceremonyState{
		kind:      ceremonyLogin,
		username:  user.Username,
		principal: user.PrincipalID,
		data:      *session,
	}); err != nil {
		writeAuthError(response, http.StatusServiceUnavailable)
		return
	}
	writeJSON(response, http.StatusOK, options)
}

func (handler *Handler) FinishLogin(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !handler.acceptPOST(response, request) {
		return
	}
	request.Body = http.MaxBytesReader(
		response,
		request.Body,
		maxAuthRequestBytes,
	)
	state, err := handler.sessions.consumeCeremony(
		response,
		request,
		ceremonyLogin,
	)
	if err != nil {
		writeAuthError(response, http.StatusUnauthorized)
		return
	}
	user, err := handler.store.LoadUser(request.Context(), state.username)
	if err != nil || user.PrincipalID != state.principal {
		writeAuthError(response, http.StatusUnauthorized)
		return
	}
	credential, err := handler.ceremony.FinishLogin(user, state.data, request)
	if err != nil ||
		credential == nil ||
		credential.Authenticator.CloneWarning {
		writeAuthError(response, http.StatusUnauthorized)
		return
	}
	now := handler.now().UTC()
	frozen, err := handler.writeFrozen(request.Context())
	if err != nil {
		writeAuthError(response, http.StatusServiceUnavailable)
		return
	}
	if !frozen {
		if err := handler.store.UpdateCredential(
			request.Context(),
			user,
			credential,
			now,
		); err != nil {
			if cutoverfreeze.IsWriteFrozen(err) {
				writeAuthFrozen(response)
				return
			}
			writeAuthError(response, http.StatusUnauthorized)
			return
		}
	}
	if err := handler.sessions.issueSession(response, user); err != nil {
		writeAuthError(response, http.StatusServiceUnavailable)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "authenticated"})
}

func (handler *Handler) BeginRegistration(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !handler.acceptPOST(response, request) {
		return
	}
	if frozen, err := handler.writeFrozen(request.Context()); err != nil {
		writeAuthError(response, http.StatusServiceUnavailable)
		return
	} else if frozen {
		writeAuthFrozen(response)
		return
	}
	username, ok := decodeUsername(response, request)
	if !ok {
		return
	}
	user, err := handler.store.LoadUser(request.Context(), username)
	if err != nil {
		writeAuthError(response, http.StatusUnauthorized)
		return
	}
	session, sessionErr := handler.sessions.authenticate(request)
	authenticated := sessionErr == nil &&
		session.principal == user.PrincipalID &&
		session.username == user.Username
	bootstrap := subtle.ConstantTimeCompare(
		[]byte(request.Header.Get("X-Hexroute-Bootstrap")),
		handler.bootstrapSecret,
	) == 1 && len(user.Credentials) == 0
	if !authenticated && !bootstrap {
		writeAuthError(response, http.StatusUnauthorized)
		return
	}
	options, webauthnSession, err := handler.ceremony.BeginRegistration(user)
	if err != nil || webauthnSession == nil {
		writeAuthError(response, http.StatusUnauthorized)
		return
	}
	if err := handler.sessions.issueCeremony(response, ceremonyState{
		kind:      ceremonyRegistration,
		username:  user.Username,
		principal: user.PrincipalID,
		data:      *webauthnSession,
	}); err != nil {
		writeAuthError(response, http.StatusServiceUnavailable)
		return
	}
	writeJSON(response, http.StatusOK, options)
}

func (handler *Handler) FinishRegistration(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !handler.acceptPOST(response, request) {
		return
	}
	if frozen, err := handler.writeFrozen(request.Context()); err != nil {
		writeAuthError(response, http.StatusServiceUnavailable)
		return
	} else if frozen {
		writeAuthFrozen(response)
		return
	}
	request.Body = http.MaxBytesReader(
		response,
		request.Body,
		maxAuthRequestBytes,
	)
	state, err := handler.sessions.consumeCeremony(
		response,
		request,
		ceremonyRegistration,
	)
	if err != nil {
		writeAuthError(response, http.StatusUnauthorized)
		return
	}
	user, err := handler.store.LoadUser(request.Context(), state.username)
	if err != nil || user.PrincipalID != state.principal {
		writeAuthError(response, http.StatusUnauthorized)
		return
	}
	credential, err := handler.ceremony.FinishRegistration(
		user,
		state.data,
		request,
	)
	if err != nil || credential == nil {
		writeAuthError(response, http.StatusUnauthorized)
		return
	}
	now := handler.now().UTC()
	if err := handler.store.AddCredential(
		request.Context(),
		user,
		credential,
		now,
	); err != nil {
		if cutoverfreeze.IsWriteFrozen(err) {
			writeAuthFrozen(response)
			return
		}
		writeAuthError(response, http.StatusUnauthorized)
		return
	}
	if err := handler.sessions.issueSession(response, user); err != nil {
		writeAuthError(response, http.StatusServiceUnavailable)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]string{"status": "registered"})
}

func (handler *Handler) writeFrozen(ctx context.Context) (bool, error) {
	if handler.freeze == nil {
		return false, nil
	}
	state, err := handler.freeze.Read(ctx)
	if err != nil {
		return false, err
	}
	return state.Frozen, nil
}

func (handler *Handler) Logout(
	response http.ResponseWriter,
	request *http.Request,
) {
	if !handler.acceptPOST(response, request) {
		return
	}
	handler.sessions.logout(response, request)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) Authorize(
	request *http.Request,
) (metadata.UUID, string, bool) {
	if handler == nil || request == nil {
		return "", "", false
	}
	session, err := handler.sessions.authenticate(request)
	if err != nil {
		return "", "", false
	}
	return session.principal, session.username, true
}

func (handler *Handler) acceptPOST(
	response http.ResponseWriter,
	request *http.Request,
) bool {
	setAuthHeaders(response)
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeAuthError(response, http.StatusMethodNotAllowed)
		return false
	}
	if request.Header.Get("Origin") != handler.origin {
		writeAuthError(response, http.StatusForbidden)
		return false
	}
	return true
}

func decodeUsername(
	response http.ResponseWriter,
	request *http.Request,
) (string, bool) {
	request.Body = http.MaxBytesReader(response, request.Body, 4096)
	var payload struct {
		Username string `json:"username"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil ||
		!validUsername(payload.Username) {
		writeAuthError(response, http.StatusBadRequest)
		return "", false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeAuthError(response, http.StatusBadRequest)
		return "", false
	}
	return payload.Username, true
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	setAuthHeaders(response)
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeAuthError(response http.ResponseWriter, status int) {
	writeJSON(response, status, map[string]string{"status": "authentication_failed"})
}

func writeAuthFrozen(response http.ResponseWriter) {
	response.Header().Set(
		"Retry-After",
		strconv.Itoa(cutoverfreeze.RetryAfterSeconds),
	)
	writeJSON(response, http.StatusServiceUnavailable, map[string]string{
		"status": "write_frozen",
	})
}

func setAuthHeaders(response http.ResponseWriter) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("Strict-Transport-Security", "max-age=31536000")
	response.Header().Set("X-Content-Type-Options", "nosniff")
}

func validOrigin(value string) bool {
	return strings.HasPrefix(value, "https://") &&
		!strings.ContainsAny(value[len("https://"):], "/?#")
}
