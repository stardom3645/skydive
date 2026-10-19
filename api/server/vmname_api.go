package server

import (
	"encoding/json"
	"net/http"

	shttp "github.com/skydive-project/skydive/graffiti/http"
)

func RegisterVmNameMapAPI(httpServer *shttp.Server, authBackend shttp.AuthenticationBackend, getMap func() map[string]string) {
	registerNetdiveAPIRoutes(httpServer, authBackend, []netdiveAPIRoute{{
		Name: "VMNameMap", Method: "GET", Path: "/api/vm-name-map", Object: "topology", Action: "read",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(getMap())
		},
	}})
}
