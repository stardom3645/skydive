package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/skydive-project/skydive/common"
	shttp "github.com/skydive-project/skydive/graffiti/http"
)

const moldManagementServerMetricsCommand = "listManagementServersMetrics"

type moldManagementServersResponse struct {
	ManagementServers []map[string]interface{} `json:"managementservers"`
}

func RegisterMoldManagementServerAPI(httpServer *shttp.Server) {
	httpServer.Router.HandleFunc("/api/mold/management-servers", handleMoldManagementServers).Methods("GET")
}

func handleMoldManagementServers(w http.ResponseWriter, _ *http.Request) {
	dbServers, dbErr := loadMoldManagementServersFromDB()
	metricServers, metricErr := loadMoldManagementServerMetrics()

	servers := mergeMoldManagementServers(dbServers, metricServers)
	if len(servers) == 0 && dbErr != nil && metricErr != nil {
		http.Error(w, "Mold management server data is unavailable.", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(moldManagementServersResponse{ManagementServers: servers})
}

func loadMoldManagementServersFromDB() ([]map[string]interface{}, error) {
	db, err := common.OpenMoldDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT m.uuid,
		       m.name,
		       m.state,
		       m.version,
		       m.service_ip,
		       m.last_update,
		       s.last_jvm_start,
		       s.last_jvm_stop,
		       s.last_system_boot,
		       s.os_distribution,
		       s.java_name,
		       s.java_version,
		       s.updated
		FROM mshost m
		LEFT JOIN mshost_status s ON s.ms_id = m.uuid
		WHERE m.removed IS NULL
		ORDER BY m.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	servers := []map[string]interface{}{}
	for rows.Next() {
		var id, name, state, version, serviceIP sql.NullString
		var lastUpdate, lastStart, lastStop, lastBoot, statusUpdated sql.NullString
		var osDistribution, javaDistribution, javaVersion sql.NullString
		if err := rows.Scan(
			&id, &name, &state, &version, &serviceIP, &lastUpdate,
			&lastStart, &lastStop, &lastBoot, &osDistribution,
			&javaDistribution, &javaVersion, &statusUpdated,
		); err != nil {
			return nil, err
		}

		server := map[string]interface{}{}
		putMoldString(server, "id", id)
		putMoldString(server, "name", name)
		putMoldString(server, "state", state)
		putMoldString(server, "version", version)
		putMoldString(server, "serviceip", serviceIP)
		putMoldString(server, "lastupdate", lastUpdate)
		putMoldString(server, "lastserverstart", lastStart)
		putMoldString(server, "lastserverstop", lastStop)
		putMoldString(server, "lastboottime", lastBoot)
		putMoldString(server, "osdistribution", osDistribution)
		putMoldString(server, "javadistribution", javaDistribution)
		putMoldString(server, "javaversion", javaVersion)
		putMoldString(server, "collectiontime", statusUpdated)
		servers = append(servers, server)
	}
	return servers, rows.Err()
}

func loadMoldManagementServerMetrics() ([]map[string]interface{}, error) {
	body, _, err := requestMoldAPI(moldManagementServerMetricsCommand, []apiParam{
		{Key: "command", Value: moldManagementServerMetricsCommand},
		{Key: "response", Value: "json"},
		{Key: "system", Value: "true"},
	})
	if err != nil {
		return nil, err
	}

	var payload interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	items := findArrayByKey(payload, "managementserver")
	servers := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if server, ok := item.(map[string]interface{}); ok {
			servers = append(servers, server)
		}
	}
	return servers, nil
}

func mergeMoldManagementServers(dbServers, metricServers []map[string]interface{}) []map[string]interface{} {
	merged := make([]map[string]interface{}, 0, len(dbServers)+len(metricServers))
	byKey := map[string]map[string]interface{}{}
	add := func(server map[string]interface{}) {
		key := moldManagementServerKey(server)
		if existing := byKey[key]; existing != nil {
			for field, value := range server {
				if value != nil && strings.TrimSpace(toMoldString(value)) != "" {
					existing[field] = value
				}
			}
			return
		}
		copy := map[string]interface{}{}
		for field, value := range server {
			copy[field] = value
		}
		byKey[key] = copy
		merged = append(merged, copy)
	}
	for _, server := range dbServers {
		add(server)
	}
	for _, server := range metricServers {
		add(server)
	}
	return merged
}

func moldManagementServerKey(server map[string]interface{}) string {
	for _, field := range []string{"id", "name", "serviceip"} {
		if value := strings.ToLower(strings.TrimSpace(toMoldString(server[field]))); value != "" {
			return field + ":" + value
		}
	}
	return "unknown"
}

func toMoldString(value interface{}) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func putMoldString(target map[string]interface{}, key string, value sql.NullString) {
	if value.Valid && strings.TrimSpace(value.String) != "" {
		target[key] = value.String
	}
}
