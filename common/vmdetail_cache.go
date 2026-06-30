package common

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
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

	query := buildVMDetailMapQuery(db)
	if query == "" {
		log.Printf("DB query failed for vm detail map: vm_instance table is missing")
		return
	}

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

func buildVMDetailMapQuery(db *sql.DB) string {
	vmColumns := moldTableColumns(db, "vm_instance")
	if len(vmColumns) == 0 {
		return ""
	}

	userVMColumns := moldTableColumns(db, "user_vm")
	hostColumns := moldTableColumns(db, "host")
	guestOSColumns := moldTableColumns(db, "guest_os")
	serviceOfferingColumns := moldTableColumns(db, "service_offering")
	joins := []string{}

	if len(userVMColumns) > 0 {
		joins = append(joins, "LEFT JOIN user_vm uv ON uv.id = v.id")
	}
	if vmColumns["host_id"] && len(hostColumns) > 0 {
		joins = append(joins, "LEFT JOIN host h ON h.id = v.host_id")
	}

	guestOSExpr := "''"
	if len(guestOSColumns) > 0 {
		switch {
		case vmColumns["guest_os_id"]:
			joins = append(joins, "LEFT JOIN guest_os go ON go.id = v.guest_os_id")
			guestOSExpr = moldCoalesceExpression("go", guestOSColumns, []string{"display_name", "display_text", "name"}, "''")
		case userVMColumns["guest_os_id"]:
			joins = append(joins, "LEFT JOIN guest_os go ON go.id = uv.guest_os_id")
			guestOSExpr = moldCoalesceExpression("go", guestOSColumns, []string{"display_name", "display_text", "name"}, "''")
		}
	}

	cpuExpr := moldCoalesceExpression("v", vmColumns, []string{"cpu", "cpus", "cpu_number", "cpu_count"}, "''")
	memoryExpr := moldCoalesceExpression("v", vmColumns, []string{"memory", "ram_size", "max_memory", "memory_total"}, "''")
	if len(serviceOfferingColumns) > 0 {
		switch {
		case vmColumns["service_offering_id"]:
			joins = append(joins, "LEFT JOIN service_offering so ON so.id = v.service_offering_id")
			cpuExpr = moldCoalesceExpression("so", serviceOfferingColumns, []string{"cpu", "cpus", "cpu_number", "cpu_count"}, cpuExpr)
			memoryExpr = moldCoalesceExpression("so", serviceOfferingColumns, []string{"ram_size", "memory", "max_memory", "memory_total"}, memoryExpr)
		case userVMColumns["service_offering_id"]:
			joins = append(joins, "LEFT JOIN service_offering so ON so.id = uv.service_offering_id")
			cpuExpr = moldCoalesceExpression("so", serviceOfferingColumns, []string{"cpu", "cpus", "cpu_number", "cpu_count"}, cpuExpr)
			memoryExpr = moldCoalesceExpression("so", serviceOfferingColumns, []string{"ram_size", "memory", "max_memory", "memory_total"}, memoryExpr)
		}
	}

	uuidExpr := moldCoalesceExpression("v", vmColumns, []string{"uuid"}, moldCoalesceExpression("uv", userVMColumns, []string{"uuid"}, "''"))
	instanceNameExpr := moldCoalesceExpression("v", vmColumns, []string{"instance_name"}, "''")
	nameExpr := moldCoalesceExpression("v", vmColumns, []string{"name"}, "''")
	displayNameExpr := moldCoalesceExpression("uv", userVMColumns, []string{"display_name"}, moldCoalesceExpression("v", vmColumns, []string{"display_name", "name"}, "''"))
	stateExpr := moldCoalesceExpression("v", vmColumns, []string{"state"}, "''")
	hostNameExpr := moldCoalesceExpression("h", hostColumns, []string{"name"}, moldCoalesceExpression("v", vmColumns, []string{"host_name", "hostname"}, "''"))
	privateIPExpr := moldCoalesceExpression("v", vmColumns, []string{"private_ip_address", "private_ip", "ip_address", "ip4_address"}, "''")

	removedFilter := ""
	if vmColumns["removed"] {
		removedFilter = "WHERE v.removed IS NULL"
	}

	return fmt.Sprintf(`
		SELECT %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s,
		       %s
		FROM vm_instance v
		%s
		%s
		ORDER BY v.id DESC`,
		uuidExpr,
		instanceNameExpr,
		nameExpr,
		displayNameExpr,
		stateExpr,
		hostNameExpr,
		privateIPExpr,
		guestOSExpr,
		cpuExpr,
		memoryExpr,
		strings.Join(joins, "\n\t\t"),
		removedFilter,
	)
}

func moldTableColumns(db *sql.DB, table string) map[string]bool {
	rows, err := db.Query("SHOW COLUMNS FROM " + table)
	if err != nil {
		return map[string]bool{}
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var field, columnType, nullValue, keyValue, defaultValue, extra sql.NullString
		if err := rows.Scan(&field, &columnType, &nullValue, &keyValue, &defaultValue, &extra); err != nil {
			continue
		}
		if field.Valid && field.String != "" {
			columns[strings.ToLower(field.String)] = true
		}
	}
	return columns
}

func moldCoalesceExpression(alias string, columns map[string]bool, candidates []string, fallback string) string {
	expressions := []string{}
	for _, column := range candidates {
		if columns[strings.ToLower(column)] {
			expressions = append(expressions, alias+"."+column)
		}
	}
	if fallback != "" && fallback != "''" {
		expressions = append(expressions, fallback)
	}
	if len(expressions) == 0 {
		return "''"
	}
	expressions = append(expressions, "''")
	return "COALESCE(" + strings.Join(expressions, ", ") + ")"
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
