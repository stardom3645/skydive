package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/skydive-project/skydive/common"
	shttp "github.com/skydive-project/skydive/graffiti/http"
)

func RegisterVMNetworkMapAPI(httpServer *shttp.Server, getMap func() map[string][]common.VMNetworkInfo, refreshInterval time.Duration) {
	httpServer.Router.HandleFunc("/api/vm-network-map", func(w http.ResponseWriter, r *http.Request) {
		common.EnsureVMNetworkMapFresh(5 * time.Second)
		lastLoad := common.GetVMNetworkMapLastLoad()
		if !lastLoad.IsZero() {
			w.Header().Set("X-VMNetworkMap-Last-Load", lastLoad.UTC().Format(time.RFC3339))
		}
		w.Header().Set("X-VMNetworkMap-Refresh-Interval", fmt.Sprintf("%ds", int(refreshInterval.Seconds())))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(getMap())
	}).Methods("GET")
}
