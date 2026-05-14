package server

import (
	"encoding/json"
	"net/http"
	"net/url"

	shttp "github.com/skydive-project/skydive/graffiti/http"
)

type moldVMConsoleResponse struct {
	URL string `json:"url"`
}

func handleMoldVMConsole(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("nodeId")
	vmID := r.URL.Query().Get("vmId")

	consoleURL := getMoldVMConsoleURL(nodeID, vmID)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(moldVMConsoleResponse{URL: consoleURL}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func getMoldVMConsoleURL(nodeID, vmID string) string {
	return buildMockMoldConsoleURL(nodeID, vmID)
}

func buildMockMoldConsoleURL(nodeID, vmID string) string {
	// TODO: 추후 Mold API 연동으로 교체
	params := url.Values{}
	params.Set("mock", "true")
	if nodeID != "" {
		params.Set("nodeId", nodeID)
	}
	if vmID != "" {
		params.Set("vmId", vmID)
	}

	return "https://mold.example.com/client/console?" + params.Encode()
}

func RegisterMoldVMConsoleAPI(httpServer *shttp.Server) {
	httpServer.Router.HandleFunc("/api/mold/vmconsole", handleMoldVMConsole).Methods("GET")
}
