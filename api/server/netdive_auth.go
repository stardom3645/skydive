package server

import (
	"net/http"

	auth "github.com/abbot/go-http-auth"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	"github.com/skydive-project/skydive/graffiti/rbac"
)

// Keep legacy handlers and their payloads unchanged. Authentication and RBAC
// must run before a handler can access Mold, Kubernetes, Wall or local caches.
type netdiveAPIRoute struct {
	Name    string
	Method  string
	Path    string
	Object  string
	Action  string
	Handler http.HandlerFunc
}

func registerNetdiveAPIRoutes(server *shttp.Server, backend shttp.AuthenticationBackend, routes []netdiveAPIRoute) {
	// noauth assigns "admin" without checking credentials. It cannot protect
	// these APIs, even when a caller supplies an arbitrary token or cookie.
	authenticationEnabled := backend != nil && backend.Name() != "noauth"
	if backend == nil {
		backend = shttp.NewNoAuthenticationBackend()
	}
	authenticatedRoutes := make([]shttp.Route, 0, len(routes))
	for _, route := range routes {
		route := route
		authenticatedRoutes = append(authenticatedRoutes, shttp.Route{
			Name: route.Name, Method: route.Method, Path: route.Path,
			HandlerFunc: func(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
				if !authenticationEnabled || r.Username == "" {
					shttp.Unauthorized(w, &r.Request, shttp.ErrWrongCredentials)
					return
				}
				if !rbac.Enforce(r.Username, route.Object, route.Action) {
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
				route.Handler(w, &r.Request)
			},
		})
	}
	server.RegisterRoutes(authenticatedRoutes, backend)
}
