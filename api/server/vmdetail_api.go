package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/skydive-project/skydive/common"
	shttp "github.com/skydive-project/skydive/graffiti/http"
)

func RegisterVMDetailMapAPI(httpServer *shttp.Server, getMap func() map[string]common.VMDetailInfo, refreshInterval time.Duration) {
	httpServer.Router.HandleFunc("/api/vm-detail-map", func(w http.ResponseWriter, r *http.Request) {
		common.EnsureVMDetailMapFresh(5 * time.Second)
		lastLoad := common.GetVMDetailMapLastLoad()
		if !lastLoad.IsZero() {
			w.Header().Set("X-VMDetailMap-Last-Load", lastLoad.UTC().Format(time.RFC3339))
		}
		w.Header().Set("X-VMDetailMap-Refresh-Interval", fmt.Sprintf("%ds", int(refreshInterval.Seconds())))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(getMap())
	}).Methods("GET")
}
