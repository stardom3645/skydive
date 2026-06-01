package common

import (
	"database/sql"
	"log"
	"sync"
	"time"
)

type VMNetworkInfo struct {
	NetworkName string `json:"networkName"`
	MACAddress  string `json:"macAddress"`
	IPAddress   string `json:"ipAddress"`
}

var (
	vmNetworkMap      = make(map[string][]VMNetworkInfo)
	vmNetworkMapLock  = sync.RWMutex{}
	vmNetworkLastLoad time.Time
)

func LoadVMNetworkMapFromCloudstack() {
	db, err := OpenMoldDB()
	if err != nil {
		log.Printf("DB connection failed for vm network map: %v", err)
		return
	}
	defer db.Close()

	query := `
		SELECT v.instance_name,
		       COALESCE(net.name, ''),
		       COALESCE(n.mac_address, ''),
		       COALESCE(n.ip4_address, '')
		FROM vm_instance v
		JOIN nics n ON n.instance_id = v.id
		LEFT JOIN networks net ON net.id = n.network_id
		WHERE v.removed IS NULL
		  AND n.removed IS NULL
		  AND (net.removed IS NULL OR net.id IS NULL)
		ORDER BY v.id DESC, n.id DESC`

	rows, err := db.Query(query)
	if err != nil {
		log.Printf("DB query failed for vm network map: %v", err)
		return
	}
	defer rows.Close()

	temp := make(map[string][]VMNetworkInfo)
	for rows.Next() {
		var instanceName, networkName, mac, ip string
		if scanErr := rows.Scan(&instanceName, &networkName, &mac, &ip); scanErr != nil {
			continue
		}
		if instanceName == "" {
			continue
		}
		temp[instanceName] = append(temp[instanceName], VMNetworkInfo{
			NetworkName: networkName,
			MACAddress:  mac,
			IPAddress:   ip,
		})
	}

	vmNetworkMapLock.Lock()
	vmNetworkMap = temp
	vmNetworkLastLoad = time.Now()
	vmNetworkMapLock.Unlock()
}

func EnsureVMNetworkMapFresh(maxAge time.Duration) {
	if maxAge <= 0 {
		return
	}

	vmNetworkMapLock.RLock()
	last := vmNetworkLastLoad
	vmNetworkMapLock.RUnlock()

	if last.IsZero() || time.Since(last) > maxAge {
		LoadVMNetworkMapFromCloudstack()
	}
}

func GetVMNetworkMap() map[string][]VMNetworkInfo {
	vmNetworkMapLock.RLock()
	defer vmNetworkMapLock.RUnlock()

	cp := make(map[string][]VMNetworkInfo, len(vmNetworkMap))
	for k, v := range vmNetworkMap {
		items := make([]VMNetworkInfo, len(v))
		copy(items, v)
		cp[k] = items
	}
	return cp
}

func StartVMNetworkMapAutoRefresh(interval time.Duration) {
	if interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			LoadVMNetworkMapFromCloudstack()
		}
	}()
}

func ResolveVMIDFromNodeIDWithDB(db *sql.DB, nodeID string) (string, error) {
	var vmID string
	query := `
		SELECT uuid
		FROM vm_instance
		WHERE removed IS NULL
		  AND (uuid = ? OR instance_name = ? OR name = ?)
		ORDER BY id DESC
		LIMIT 1`
	err := db.QueryRow(query, nodeID, nodeID, nodeID).Scan(&vmID)
	if err != nil {
		return "", err
	}
	return vmID, nil
}
