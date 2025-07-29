package server

import (
	"encoding/json"
	"net/http"

	shttp "github.com/skydive-project/skydive/graffiti/http"
)

func RegisterVmNameMapAPI(httpServer *shttp.Server, getMap func() map[string]string) {
	httpServer.Router.HandleFunc("/api/vm-name-map", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(getMap())
	}).Methods("GET")
}