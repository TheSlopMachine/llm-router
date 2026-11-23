package dashboard

import "net/http"

// registerDockerPlatformRoutes adds nothing: a FreeBSD router reaches Docker
// only on another machine.
func (h *Handler) registerDockerPlatformRoutes(*http.ServeMux) {}
