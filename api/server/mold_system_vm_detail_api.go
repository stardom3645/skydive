package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type moldSystemVMDetail struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	SystemVMType         string `json:"systemVmType"`
	State                string `json:"state,omitempty"`
	AgentState           string `json:"agentState,omitempty"`
	ActiveViewerSessions *int64 `json:"activeViewerSessions,omitempty"`
}

type moldSystemVMDetailResponse struct {
	MoldMatched bool                `json:"moldMatched"`
	SystemVM    *moldSystemVMDetail `json:"systemVM,omitempty"`
	Message     string              `json:"message,omitempty"`
}

func handleMoldSystemVMDetail(w http.ResponseWriter, r *http.Request) {
	id, name := strings.TrimSpace(r.URL.Query().Get("id")), strings.TrimSpace(r.URL.Query().Get("name"))
	write := func(status int, response moldSystemVMDetailResponse) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(response)
	}
	if id == "" && name == "" {
		write(http.StatusBadRequest, moldSystemVMDetailResponse{Message: "id or exact system VM name is required"})
		return
	}
	params := []apiParam{{Key: "command", Value: moldHostSystemVMListCommand}, {Key: "response", Value: "json"}}
	if id != "" {
		params = append(params, apiParam{Key: "id", Value: id})
	} else {
		params = append(params, apiParam{Key: "name", Value: name})
	}
	items, err := requestMoldItems(moldHostSystemVMListCommand, "systemvm", params)
	if err != nil {
		write(http.StatusBadGateway, moldSystemVMDetailResponse{Message: "Mold system VM metadata is unavailable"})
		return
	}
	detail, err := pickMoldSystemVMDetail(items, id, name)
	if err != nil {
		write(http.StatusBadGateway, moldSystemVMDetailResponse{Message: err.Error()})
		return
	}
	write(http.StatusOK, moldSystemVMDetailResponse{MoldMatched: detail != nil, SystemVM: detail})
}

// UUID is authoritative; never substitute a same-named VM after an ID mismatch.
// Name lookup is exact even if Mold returns substring matches.
func pickMoldSystemVMDetail(items []interface{}, id, name string) (*moldSystemVMDetail, error) {
	var result *moldSystemVMDetail
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		vmType := strings.ToLower(moldHostValueAsString(m["systemvmtype"]))
		if vmType != "consoleproxy" && vmType != "secondarystoragevm" {
			continue
		}
		vmID, vmName := moldHostValueAsString(m["id"]), moldHostValueAsString(m["name"])
		if (id != "" && !strings.EqualFold(vmID, id)) || (id == "" && name != vmName) || (id == "" && name == "") {
			continue
		}
		if result != nil {
			return nil, fmt.Errorf("multiple system VMs match; an exact UUID is required")
		}
		result = &moldSystemVMDetail{ID: vmID, Name: vmName, SystemVMType: vmType,
			State: moldHostValueAsString(m["state"]), AgentState: moldHostValueAsString(m["agentstate"])}
		if vmType == "consoleproxy" {
			count, err := strconv.ParseInt(moldHostValueAsString(m["activeviewersessions"]), 10, 64)
			if err == nil && count >= 0 && count <= 9007199254740991 {
				result.ActiveViewerSessions = &count
			}
		}
	}
	return result, nil
}
