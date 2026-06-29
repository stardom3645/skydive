package common

import (
	"database/sql"
	"log"
	"sync"
	"time"
)

type VMDetailInfo struct {
	UUID         string `json:"uuid"`
	InstanceName string `json:"instanceName"`
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	State        string `json:"state"`
	HostName     string `json:"hostName"`
	PrivateIP    string `json:"privateIp"`
	GuestOS      string `json:"guestOS"`
	CPUNumber    string `json:"cpuNumber"`
	Memory       string `json:"memory"`
}

var (
	vmDetailMap      = make(map[string]VMDetailInfo)
	vmDetailMapLock  = sync.RWMutex{}
	vmDetailLastLoad time.Time
)

func LoadVMDetailMapFromCloudstack() {
	db, err := OpenMoldDB()
	if err != nil {
		log.Printf("DB connection failed for vm detail map: %v", err)
		return
	}
	defer db.Close()

	query := `
		SELECT COALESCE(v.uuid, ''),
		       COALESCE(v.instance_name, ''),
		       COALESCE(v.name, ''),
		       COALESCE(uv.display_name, ''),
		       COALESCE(v.state, ''),
		       COALESCE(h.name, ''),
		       COALESCE(v.private_ip_address, ''),
		       COALESCE(go.display_name, ''),
		       COALESCE(so.cpu, ''),
		       COALESCE(so.ram_size, '')
		FROM vm_instance v
		LEFT JOIN user_vm uv ON uv.id = v.id
		LEFT JOIN host h ON h.id = v.host_id
		LEFT JOIN guest_os go ON go.id = uv.guest_os_id
		LEFT JOIN service_offering so ON so.id = v.service_offering_id
		WHERE v.removed IS NULL
		ORDER BY v.id DESC`

	rows, err := db.Query(query)
	if err != nil {
		log.Printf("DB query failed for vm detail map: %v", err)
		return
	}
	defer rows.Close()

	temp := make(map[string]VMDetailInfo)
	for rows.Next() {
		var item VMDetailInfo
		if scanErr := rows.Scan(
			&item.UUID,
			&item.InstanceName,
			&item.Name,
			&item.DisplayName,
			&item.State,
			&item.HostName,
			&item.PrivateIP,
			&item.GuestOS,
			&item.CPUNumber,
			&item.Memory,
		); scanErr != nil {
			if scanErr != sql.ErrNoRows {
				log.Printf("DB scan failed for vm detail map: %v", scanErr)
			}
			continue
		}

		for _, key := range uniqueVMDetailKeys(item) {
			temp[key] = item
		}
	}

	vmDetailMapLock.Lock()
	vmDetailMap = temp
	vmDetailLastLoad = time.Now()
	vmDetailMapLock.Unlock()
}

func EnsureVMDetailMapFresh(maxAge time.Duration) {
	if maxAge <= 0 {
		return
	}

	vmDetailMapLock.RLock()
	last := vmDetailLastLoad
	vmDetailMapLock.RUnlock()

	if last.IsZero() || time.Since(last) > maxAge {
		LoadVMDetailMapFromCloudstack()
	}
}

func GetVMDetailMap() map[string]VMDetailInfo {
	vmDetailMapLock.RLock()
	defer vmDetailMapLock.RUnlock()

	cp := make(map[string]VMDetailInfo, len(vmDetailMap))
	for k, v := range vmDetailMap {
		cp[k] = v
	}
	return cp
}

func GetVMDetailMapLastLoad() time.Time {
	vmDetailMapLock.RLock()
	defer vmDetailMapLock.RUnlock()
	return vmDetailLastLoad
}

func StartVMDetailMapAutoRefresh(interval time.Duration) {
	if interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			LoadVMDetailMapFromCloudstack()
		}
	}()
}

func uniqueVMDetailKeys(item VMDetailInfo) []string {
	seen := make(map[string]struct{})
	values := []string{item.UUID, item.InstanceName, item.Name, item.DisplayName}
	keys := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		keys = append(keys, value)
	}
	return keys
}
