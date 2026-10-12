package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/requestsource"
	"github.com/TheSlopMachine/llm-router/internal/services/admin"
)

// apiStatus returns system status
// @Summary      Get system status
// @Description  Returns bootstrap status, authentication status, and system statistics.
// @Tags         System
// @Produce      json
// @Success      200 {object} object{bootstrapped=bool,authenticated=bool,stats=object{providers=int,tokens=int,credentials=int}}
// @Router       /api/llm-router/status [get]
func (h *Handler) apiStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The admin listener reports bootstrapped and authenticated without
		// an account or a session; the source decides, never a flag.
		private := requestsource.SourceFromContext(r.Context()) == requestsource.RequestSourcePrivate
		realBootstrapped, _ := h.gormAdminService.IsBootstrapped(r.Context())
		bootstrapped := EffectiveBootstrapped(realBootstrapped, private)
		authenticated := private
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
			"bootstrapped":  bootstrapped,
			"authenticated": authenticated,
			"stats":         statistics,
		})
	}
}

// apiShutdown initiates graceful shutdown. Admin listener only: public
// requests answer 404 as if the route did not exist. The loopback bind
// is the only protection, so this never requires authentication or
// bootstrapping. The signal is non-blocking; the server answers 200
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
	if h.logger != nil {
		h.logger.Info("shutdown requested via admin listener")
	}
	if h.requestShutdown != nil {
		h.requestShutdown()
	}
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}

// apiLogin authenticates admin user
// @Summary      Admin login
// @Description  Authenticates admin user and returns session cookie.
// @Tags         Auth
// @Accept       json
// @Produce      json
// @Param        credentials body object{username=string,password=string,remember_me=bool} true "Login credentials"
// @Success      200 {object} object{message=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Router       /api/llm-router/login [post]
func (h *Handler) apiLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		RememberMe bool   `json:"remember_me"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sessionToken, expiresAt, err := h.gormAdminService.Login(r.Context(), body.Username, body.Password, body.RememberMe)
	if err != nil {
		h.jsonErr(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}

// apiLogout logs out admin user
// @Summary      Admin logout
// @Description  Invalidates the current session.
// @Tags         Auth
// @Produce      json
// @Security     SessionAuth
// @Router       /api/llm-router/logout [post]
func (h *Handler) apiLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		h.gormAdminService.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, MaxAge: -1, Path: "/"})
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}

// apiBootstrap creates initial admin account
// @Summary      Bootstrap system
// @Description  Creates the initial admin account. Only works if system is not bootstrapped.
// @Tags         System
// @Accept       json
// @Produce      json
// @Param        account body object{username=string,password=string} true "Admin account"
// @Success      200 {object} object{ok=bool}
// @Failure      400 {object} models.ErrorResponse
// @Router       /api/llm-router/bootstrap [post]
func (h *Handler) apiBootstrap(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.gormAdminService.Bootstrap(r.Context(), body.Username, body.Password); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}

// apiAdminChangePassword updates the authenticated admin's password.
// @Summary      Change admin password
// @Description  Verifies current password and sets a new one for the active admin.
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
	username := h.sessionUser(r)
	if username == "" {
		h.jsonErr(w, http.StatusUnauthorized, "session required")
		return
	}
	if err := h.gormAdminService.ChangePassword(r.Context(), username, body.CurrentPassword, body.NewPassword); err != nil {
		if errors.Is(err, admin.ErrInvalidCredentials) {
			h.jsonErr(w, http.StatusUnauthorized, "current password is incorrect")
			return
		}
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logger.Info("admin password changed", "username", username)
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}
