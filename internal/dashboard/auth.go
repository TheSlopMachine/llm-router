package dashboard

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/TheSlopMachine/llm-router/internal/requestsource"
	"github.com/TheSlopMachine/llm-router/internal/services/admin"
)

// apiStatus returns system status
// @Summary      Get system status
// @Description  Returns authentication status and system statistics.
// @Tags         System
// @Produce      json
// @Success      200 {object} object{authenticated=bool,stats=object{providers=int,tokens=int,credentials=int}}
// @Router       /api/llm-router/status [get]
func (h *Handler) apiStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The admin listener reports authenticated without a session; the
		// source decides, never a flag.
		authenticated := requestsource.SourceFromContext(r.Context()) == requestsource.RequestSourcePrivate
		if !authenticated {
			if c, err := r.Cookie(sessionCookie); err == nil {
				_, authenticated = h.gormAdminService.ValidateSession(r.Context(), c.Value)
			}
		}

		var statistics map[string]int
		if authenticated {
			providers, _ := h.gormProviderService.List(r.Context())
			tokens, _ := h.gormTokenService.List(r.Context())
			credentials, _ := h.gormCredentialService.ListAll(r.Context())
			statistics = map[string]int{
				"providers":   len(providers),
				"tokens":      len(tokens),
				"credentials": len(credentials),
			}
		}

		h.json(w, http.StatusOK, map[string]any{
			"authenticated": authenticated,
			"stats":         statistics,
		})
	}
}

// apiShutdown initiates graceful shutdown. Admin listener only: public
// requests answer 404 as if the route did not exist. The loopback bind
// is the only protection, so this never requires authentication. The signal is non-blocking; the server answers 200
// first and shuts down after the response is delivered.
// @Summary      Request server shutdown
// @Description  Signals graceful shutdown. Admin listener only; public requests 404.
// @Tags         System
// @Produce      json
// @Success      200 {object} object{ok=bool}
// @Router       /api/llm-router/shutdown [post]
func (h *Handler) apiShutdown(w http.ResponseWriter, r *http.Request) {
	if requestsource.SourceFromContext(r.Context()) != requestsource.RequestSourcePrivate {
		WriteUnknownEndpoint(w, r)
		return
	}
	h.logInfo("Shutdown requested via admin listener")
	if h.requestShutdown != nil {
		h.requestShutdown()
	}
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}

// apiLogin checks the dashboard password and starts a session
// @Summary      Dashboard login
// @Description  Checks the dashboard password and sets the session cookie. Without remember_me the cookie ends with the browser session.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        credentials body object{password=string,remember_me=bool} true "Login credentials"
// @Success      200 {object} object{ok=bool}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      429 {object} models.ErrorResponse
// @Router       /api/llm-router/login [post]
func (h *Handler) apiLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password   string `json:"password"`
		RememberMe bool   `json:"remember_me"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	limiterKey := loginLimiterKey(r)
	if wait := h.loginLimiter.retryAfter(limiterKey); wait > 0 {
		h.logWarn("Login throttled", "reason", "login_throttled", "wait", wait.String())
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
		h.jsonErr(w, http.StatusTooManyRequests, "too many failed logins")
		return
	}
	sessionToken, expiresAt, err := h.gormAdminService.Login(r.Context(), body.Password, body.RememberMe)
	if err != nil {
		if !errors.Is(err, admin.ErrInvalidCredentials) {
			h.logError("Login failed", "error", err)
			h.jsonErr(w, http.StatusInternalServerError, "login failed")
			return
		}
		h.loginLimiter.recordFailure(limiterKey)
		h.jsonErr(w, http.StatusUnauthorized, "invalid password")
		return
	}
	h.loginLimiter.recordSuccess(limiterKey)
	setSessionCookie(w, r, sessionToken, admin.Session{ExpiresAt: expiresAt, RememberMe: body.RememberMe})
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}

// setSessionCookie writes the session cookie. A remembered session gets a
// persistent cookie that expires with the server-side session; any other
// session gets a browser-session cookie, so closing the browser signs out.
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, session admin.Session) {
	cookie := &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
	}
	if session.RememberMe {
		cookie.Expires = session.ExpiresAt
	}
	http.SetCookie(w, cookie)
}

// apiLogout ends the dashboard session
// @Summary      Dashboard logout
// @Description  Invalidates the current session.
// @Tags         Auth
// @Produce      json
// @Security     SessionAuth
// @Router       /api/llm-router/logout [post]
func (h *Handler) apiLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		h.gormAdminService.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, MaxAge: -1, Path: "/", HttpOnly: true, Secure: requestIsSecure(r), SameSite: http.SameSiteLaxMode})
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}

// apiAdminChangePassword updates the dashboard password.
// @Summary      Change dashboard password
// @Description  Verifies the current password and sets a new one. Existing sessions survive.
// @Tags         Admin
// @Accept       json
// @Produce      json
// @Param        passwords body object{current_password=string,new_password=string} true "Password payload"
// @Success      200 {object} object{ok=bool}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/admin/password [post]
func (h *Handler) apiAdminChangePassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.gormAdminService.ChangePassword(r.Context(), body.CurrentPassword, body.NewPassword); err != nil {
		if errors.Is(err, admin.ErrInvalidCredentials) {
			h.jsonErr(w, http.StatusUnauthorized, "current password is incorrect")
			return
		}
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logInfo("Dashboard password changed")
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}
