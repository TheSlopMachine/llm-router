package dashboard

import (
	"encoding/json"
	"net/http"
)

// apiStatus returns system status
// @Summary      Get system status
// @Description  Returns bootstrap status, authentication status, and system statistics.
// @Tags         System
// @Produce      json
// @Success      200 {object} object{bootstrapped=bool,authenticated=bool,stats=object{providers=int,tokens=int,credentials=int}}
// @Router       /api/llm-router/status [get]
func (h *Handler) apiStatus(db interface{ IsBootstrapped() (bool, error) }) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bootstrapped, _ := db.IsBootstrapped()
		authenticated := false
		if c, err := r.Cookie(sessionCookie); err == nil {
			_, authenticated = h.adminSvc.ValidateSession(c.Value)
		}

		var stats map[string]int
		if authenticated {
			providers, _ := h.providerSvc.List()
			toks, _ := h.tokenSvc.List()
			creds, _ := h.credSvc.ListAll()
			stats = map[string]int{
				"providers":   len(providers),
				"tokens":      len(toks),
				"credentials": len(creds),
			}
		}

		h.json(w, http.StatusOK, map[string]any{
			"bootstrapped":  bootstrapped,
			"authenticated": authenticated,
			"stats":         stats,
		})
	}
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
	sessionToken, expiresAt, err := h.adminSvc.Login(body.Username, body.Password, body.RememberMe)
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
// @Success      200 {object} object{ok=bool}
// @Security     SessionAuth
// @Router       /api/llm-router/logout [post]
func (h *Handler) apiLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		h.adminSvc.Logout(c.Value)
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
	if err := h.adminSvc.Bootstrap(body.Username, body.Password); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}

