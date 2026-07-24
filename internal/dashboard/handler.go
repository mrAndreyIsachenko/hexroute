package dashboard

import (
	"html/template"
	"io"
	"net/http"
	"strconv"
	"time"
)

type AuthEndpoints interface {
	Authorizer
	BeginLogin(http.ResponseWriter, *http.Request)
	FinishLogin(http.ResponseWriter, *http.Request)
	BeginRegistration(http.ResponseWriter, *http.Request)
	FinishRegistration(http.ResponseWriter, *http.Request)
	Logout(http.ResponseWriter, *http.Request)
}

type Handler struct {
	store      Store
	authorizer Authorizer
	now        func() time.Time
	page       *template.Template
}

type pageData struct {
	Username string
	Snapshot Snapshot
}

func NewHandler(
	store Store,
	authorizer Authorizer,
	now func() time.Time,
) (*Handler, error) {
	if store == nil || authorizer == nil {
		return nil, ErrInvalidDashboard
	}
	if now == nil {
		now = time.Now
	}
	page, err := template.New("dashboard").Funcs(template.FuncMap{
		"time":  formatTime,
		"ratio": formatRatio,
		"class": statusClass,
	}).Parse(dashboardHTML)
	if err != nil {
		return nil, ErrInvalidDashboard
	}
	return &Handler{
		store:      store,
		authorizer: authorizer,
		now:        now,
		page:       page,
	}, nil
}

func NewRouter(
	pages *Handler,
	auth AuthEndpoints,
) (http.Handler, error) {
	if pages == nil || auth == nil {
		return nil, ErrInvalidDashboard
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", pages.Dashboard)
	mux.HandleFunc("GET /login", pages.Login)
	mux.HandleFunc("GET /assets/app.css", serveCSS)
	mux.HandleFunc("GET /assets/app.js", serveJavaScript)
	mux.HandleFunc("POST /auth/login/begin", auth.BeginLogin)
	mux.HandleFunc("POST /auth/login/finish", auth.FinishLogin)
	mux.HandleFunc("POST /auth/register/begin", auth.BeginRegistration)
	mux.HandleFunc("POST /auth/register/finish", auth.FinishRegistration)
	mux.HandleFunc("POST /auth/logout", auth.Logout)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" &&
			request.URL.Path != "/login" &&
			request.URL.Path != "/assets/app.css" &&
			request.URL.Path != "/assets/app.js" &&
			request.URL.Path != "/auth/login/begin" &&
			request.URL.Path != "/auth/login/finish" &&
			request.URL.Path != "/auth/register/begin" &&
			request.URL.Path != "/auth/register/finish" &&
			request.URL.Path != "/auth/logout" {
			setPageHeaders(response)
			http.NotFound(response, request)
			return
		}
		mux.ServeHTTP(response, request)
	}), nil
}

func (handler *Handler) Dashboard(
	response http.ResponseWriter,
	request *http.Request,
) {
	setPageHeaders(response)
	_, username, ok := handler.authorizer.Authorize(request)
	if !ok {
		http.Redirect(response, request, "/login", http.StatusSeeOther)
		return
	}
	snapshot, err := handler.store.Load(request.Context(), handler.now().UTC())
	if err != nil {
		response.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(response, unavailableHTML)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := handler.page.Execute(response, pageData{
		Username: username,
		Snapshot: snapshot,
	}); err != nil {
		return
	}
}

func (handler *Handler) Login(
	response http.ResponseWriter,
	request *http.Request,
) {
	setPageHeaders(response)
	if _, _, ok := handler.authorizer.Authorize(request); ok {
		http.Redirect(response, request, "/", http.StatusSeeOther)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(response, loginHTML)
}

func serveCSS(response http.ResponseWriter, request *http.Request) {
	setPageHeaders(response)
	response.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = io.WriteString(response, appCSS)
}

func serveJavaScript(response http.ResponseWriter, request *http.Request) {
	setPageHeaders(response)
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = io.WriteString(response, appJavaScript)
}

func setPageHeaders(response http.ResponseWriter) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set(
		"Content-Security-Policy",
		"default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; form-action 'none'; frame-ancestors 'none'; base-uri 'none'",
	)
	response.Header().Set("Referrer-Policy", "no-referrer")
	response.Header().Set("Strict-Transport-Security", "max-age=31536000")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("X-Frame-Options", "DENY")
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "never"
	}
	return value.UTC().Format("2006-01-02 15:04:05 UTC")
}

func formatRatio(value SLO) string {
	if value.TotalCount > 0 {
		return percent(float64(value.QualifyingCount) / float64(value.TotalCount))
	}
	if value.EligibleMilliseconds > 0 {
		return percent(float64(value.GoodMilliseconds) / float64(value.EligibleMilliseconds))
	}
	return "n/a"
}

func percent(value float64) string {
	return strconv.FormatFloat(value*100, 'f', 3, 64) + "%"
}

func statusClass(value string) string {
	switch value {
	case "ready", "healthy", "resolved", "active", "proven":
		return "ok"
	case "warning", "acknowledged", "staged":
		return "warn"
	default:
		return "bad"
	}
}
