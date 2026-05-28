package server

import (
	"encoding/json"
	"net/http"

	"github.com/skydive-project/skydive/common"
	shttp "github.com/skydive-project/skydive/graffiti/http"
)

func RegisterVMNetworkMapAPI(httpServer *shttp.Server, getMap func() map[string][]common.VMNetworkInfo) {
	httpServer.Router.HandleFunc("/api/vm-network-map", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(getMap())
	}).Methods("GET")
}
