package dashboardauth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/mrAndreyIsachenko/hexroute/internal/cutoverfreeze"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const authPrincipalID = metadata.UUID("11111111-1111-4111-8111-111111111111")

func TestHandlerRequiresOriginAndCreatesOneTimeLoginSession(t *testing.T) {
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	store := &authStoreFixture{user: authUserFixture(true)}
	ceremony := &ceremonyFixture{credential: credentialFixture()}
	handler := authHandlerFixture(t, store, ceremony, now)

	wrongOrigin := httptest.NewRequest(
		http.MethodPost,
		"/auth/login/begin",
		bytes.NewBufferString(`{"username":"operator"}`),
	)
	wrongOrigin.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	handler.BeginLogin(response, wrongOrigin)
	if response.Code != http.StatusForbidden {
		t.Fatalf("wrong-origin status = %d", response.Code)
	}

	begin := authRequest(http.MethodPost, "/auth/login/begin", `{"username":"operator"}`)
	response = httptest.NewRecorder()
	handler.BeginLogin(response, begin)
	if response.Code != http.StatusOK {
		t.Fatalf("begin status = %d body=%s", response.Code, response.Body.String())
	}
	ceremonyCookie := findCookie(t, response.Result().Cookies(), ceremonyCookieName)
	if !ceremonyCookie.Secure ||
		!ceremonyCookie.HttpOnly ||
		ceremonyCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("ceremony cookie = %+v", ceremonyCookie)
	}

	finish := authRequest(http.MethodPost, "/auth/login/finish", `{}`)
	finish.AddCookie(ceremonyCookie)
	response = httptest.NewRecorder()
	handler.FinishLogin(response, finish)
	if response.Code != http.StatusOK || store.updates != 1 {
		t.Fatalf("finish = %d updates=%d body=%s", response.Code, store.updates, response.Body.String())
	}
	sessionCookie := findCookie(t, response.Result().Cookies(), sessionCookieName)
	authorized := httptest.NewRequest(http.MethodGet, "https://status.example/", nil)
	authorized.AddCookie(sessionCookie)
	principalID, username, ok := handler.Authorize(authorized)
	if !ok || principalID != authPrincipalID || username != "operator" {
		t.Fatalf("Authorize() = %s %q %t", principalID, username, ok)
	}

	replay := authRequest(http.MethodPost, "/auth/login/finish", `{}`)
	replay.AddCookie(ceremonyCookie)
	response = httptest.NewRecorder()
	handler.FinishLogin(response, replay)
	if response.Code != http.StatusUnauthorized || store.updates != 1 {
		t.Fatalf("replay = %d updates=%d", response.Code, store.updates)
	}
}

type authFreezeReaderFunc func(context.Context) (cutoverfreeze.State, error)

func (function authFreezeReaderFunc) Read(ctx context.Context) (cutoverfreeze.State, error) {
	return function(ctx)
}

func TestFrozenLoginCreatesSessionWithoutCredentialWrite(t *testing.T) {
	now := time.Date(2026, time.July, 27, 1, 0, 0, 0, time.UTC)
	store := &authStoreFixture{user: authUserFixture(true)}
	handler := authHandlerFixture(t, store, &ceremonyFixture{credential: credentialFixture()}, now)
	handler.freeze = authFreezeReaderFunc(func(context.Context) (cutoverfreeze.State, error) {
		return cutoverfreeze.State{Frozen: true}, nil
	})

	begin := authRequest(http.MethodPost, "/auth/login/begin", `{"username":"operator"}`)
	response := httptest.NewRecorder()
	handler.BeginLogin(response, begin)
	ceremonyCookie := findCookie(t, response.Result().Cookies(), ceremonyCookieName)
	finish := authRequest(http.MethodPost, "/auth/login/finish", `{}`)
	finish.AddCookie(ceremonyCookie)
	response = httptest.NewRecorder()
	handler.FinishLogin(response, finish)
	if response.Code != http.StatusOK || store.updates != 0 {
		t.Fatalf("frozen login=%d updates=%d body=%q", response.Code, store.updates, response.Body.String())
	}
	sessionCookie := findCookie(t, response.Result().Cookies(), sessionCookieName)
	logout := authRequest(http.MethodPost, "/auth/logout", `{}`)
	logout.AddCookie(sessionCookie)
	response = httptest.NewRecorder()
	handler.Logout(response, logout)
	if response.Code != http.StatusNoContent {
		t.Fatalf("frozen logout=%d", response.Code)
	}
}

func TestFrozenRegistrationIsRetryableAndWriteFree(t *testing.T) {
	now := time.Date(2026, time.July, 27, 1, 0, 0, 0, time.UTC)
	store := &authStoreFixture{user: authUserFixture(false)}
	handler := authHandlerFixture(t, store, &ceremonyFixture{credential: credentialFixture()}, now)
	handler.freeze = authFreezeReaderFunc(func(context.Context) (cutoverfreeze.State, error) {
		return cutoverfreeze.State{Frozen: true}, nil
	})
	request := authRequest(http.MethodPost, "/auth/register/begin", `{"username":"operator"}`)
	request.Header.Set("X-Hexroute-Bootstrap", "0123456789abcdef0123456789abcdef")
	response := httptest.NewRecorder()
	handler.BeginRegistration(response, request)
	if response.Code != http.StatusServiceUnavailable || store.adds != 0 ||
		response.Header().Get("Retry-After") != "60" ||
		response.Body.String() != `{"status":"write_frozen"}`+"\n" {
		t.Fatalf("frozen registration=%d adds=%d retry=%q body=%q", response.Code, store.adds, response.Header().Get("Retry-After"), response.Body.String())
	}
}

func TestBootstrapRegistersOnlyFirstPasskey(t *testing.T) {
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	store := &authStoreFixture{user: authUserFixture(false)}
	ceremony := &ceremonyFixture{credential: credentialFixture()}
	handler := authHandlerFixture(t, store, ceremony, now)

	begin := authRequest(http.MethodPost, "/auth/register/begin", `{"username":"operator"}`)
	begin.Header.Set("X-Hexroute-Bootstrap", "0123456789abcdef0123456789abcdef")
	response := httptest.NewRecorder()
	handler.BeginRegistration(response, begin)
	if response.Code != http.StatusOK {
		t.Fatalf("bootstrap begin = %d body=%s", response.Code, response.Body.String())
	}
	ceremonyCookie := findCookie(t, response.Result().Cookies(), ceremonyCookieName)
	finish := authRequest(http.MethodPost, "/auth/register/finish", `{}`)
	finish.AddCookie(ceremonyCookie)
	response = httptest.NewRecorder()
	handler.FinishRegistration(response, finish)
	if response.Code != http.StatusCreated || store.adds != 1 {
		t.Fatalf("bootstrap finish = %d adds=%d", response.Code, store.adds)
	}
	sessionCookie := findCookie(t, response.Result().Cookies(), sessionCookieName)

	second := authRequest(http.MethodPost, "/auth/register/begin", `{"username":"operator"}`)
	second.AddCookie(sessionCookie)
	response = httptest.NewRecorder()
	handler.BeginRegistration(response, second)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated registration = %d", response.Code)
	}

	reusedBootstrap := authRequest(
		http.MethodPost,
		"/auth/register/begin",
		`{"username":"operator"}`,
	)
	reusedBootstrap.Header.Set(
		"X-Hexroute-Bootstrap",
		"0123456789abcdef0123456789abcdef",
	)
	response = httptest.NewRecorder()
	handler.BeginRegistration(response, reusedBootstrap)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("reused bootstrap status = %d", response.Code)
	}
}

func TestCloneWarningDoesNotCreateSession(t *testing.T) {
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	store := &authStoreFixture{user: authUserFixture(true)}
	credential := credentialFixture()
	credential.Authenticator.CloneWarning = true
	handler := authHandlerFixture(
		t,
		store,
		&ceremonyFixture{credential: credential},
		now,
	)
	begin := authRequest(http.MethodPost, "/auth/login/begin", `{"username":"operator"}`)
	response := httptest.NewRecorder()
	handler.BeginLogin(response, begin)
	cookie := findCookie(t, response.Result().Cookies(), ceremonyCookieName)
	finish := authRequest(http.MethodPost, "/auth/login/finish", `{}`)
	finish.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.FinishLogin(response, finish)
	if response.Code != http.StatusUnauthorized || store.updates != 0 {
		t.Fatalf("clone warning = %d updates=%d", response.Code, store.updates)
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.MaxAge > 0 {
			t.Fatalf("clone warning issued session cookie")
		}
	}
}

func TestWebAuthnCeremonyBindsFinalHTTPSOrigin(t *testing.T) {
	if _, err := NewWebAuthnCeremony(
		"status.example",
		"https://status.example",
	); err != nil {
		t.Fatalf("NewWebAuthnCeremony() error = %v", err)
	}
	if _, err := NewWebAuthnCeremony(
		"status.example",
		"http://status.example",
	); err == nil {
		t.Fatal("NewWebAuthnCeremony(http origin) succeeded")
	}
}

type authStoreFixture struct {
	user    User
	adds    int
	updates int
}

func (store *authStoreFixture) LoadUser(context.Context, string) (User, error) {
	return store.user, nil
}

func (store *authStoreFixture) AddCredential(
	_ context.Context,
	_ User,
	credential *webauthn.Credential,
	_ time.Time,
) error {
	store.adds++
	store.user.Credentials = append(store.user.Credentials, *credential)
	return nil
}

func (store *authStoreFixture) UpdateCredential(
	context.Context,
	User,
	*webauthn.Credential,
	time.Time,
) error {
	store.updates++
	return nil
}

type ceremonyFixture struct {
	credential webauthn.Credential
}

func (*ceremonyFixture) BeginRegistration(
	User,
) (*protocol.CredentialCreation, *webauthn.SessionData, error) {
	return &protocol.CredentialCreation{}, &webauthn.SessionData{
		Challenge: "registration",
	}, nil
}

func (fixture *ceremonyFixture) FinishRegistration(
	User,
	webauthn.SessionData,
	*http.Request,
) (*webauthn.Credential, error) {
	value := fixture.credential
	return &value, nil
}

func (*ceremonyFixture) BeginLogin(
	User,
) (*protocol.CredentialAssertion, *webauthn.SessionData, error) {
	return &protocol.CredentialAssertion{}, &webauthn.SessionData{
		Challenge: "login",
	}, nil
}

func (fixture *ceremonyFixture) FinishLogin(
	User,
	webauthn.SessionData,
	*http.Request,
) (*webauthn.Credential, error) {
	value := fixture.credential
	return &value, nil
}

func authHandlerFixture(
	t *testing.T,
	store Store,
	ceremony Ceremony,
	now time.Time,
) *Handler {
	t.Helper()
	randomBytes := make([]byte, 32*16)
	for index := range randomBytes {
		randomBytes[index] = byte(index)
	}
	handler, err := NewHandler(Config{
		Store:           store,
		Ceremony:        ceremony,
		Origin:          "https://status.example",
		BootstrapSecret: "0123456789abcdef0123456789abcdef",
		Random:          bytes.NewReader(randomBytes),
		Now:             func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}

func authRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(
		method,
		"https://status.example"+path,
		bytes.NewBufferString(body),
	)
	request.Header.Set("Origin", "https://status.example")
	request.Header.Set("Content-Type", "application/json")
	return request
}

func findCookie(
	t *testing.T,
	cookies []*http.Cookie,
	name string,
) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name && cookie.MaxAge > 0 {
			return cookie
		}
	}
	encoded, _ := json.Marshal(cookies)
	t.Fatalf("cookie %q not found in %s", name, encoded)
	return nil
}

func authUserFixture(withCredential bool) User {
	user := User{
		PrincipalID: authPrincipalID,
		Username:    "operator",
		DisplayName: "Operator",
		UserHandle:  bytes.Repeat([]byte{1}, 32),
		Enabled:     true,
	}
	if withCredential {
		user.Credentials = []webauthn.Credential{credentialFixture()}
	}
	return user
}

func credentialFixture() webauthn.Credential {
	return webauthn.Credential{
		ID:        bytes.Repeat([]byte{2}, 32),
		PublicKey: []byte{1, 2, 3},
		Flags: webauthn.CredentialFlags{
			UserPresent:  true,
			UserVerified: true,
		},
	}
}
