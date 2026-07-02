package common

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type VMNetworkInfo struct {
	NetworkName     string `json:"networkName"`
	MACAddress      string `json:"macAddress"`
	IPAddress       string `json:"ipAddress"`
	NetworkType     string `json:"networkType"`
	TrafficType     string `json:"trafficType"`
	Gateway         string `json:"gateway"`
	CIDR            string `json:"cidr"`
	BroadcastURI    string `json:"broadcastUri"`
	IsDefault       string `json:"isDefault"`
	NetworkOffering string `json:"networkOffering"`
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

	query := buildVMNetworkMapQuery(db)
	if query == "" {
		log.Printf("DB query failed for vm network map: required tables are missing")
		return
	}

	rows, err := db.Query(query)
	if err != nil {
		log.Printf("DB query failed for vm network map: %v", err)
		return
	}
	defer rows.Close()

	temp := make(map[string][]VMNetworkInfo)
	for rows.Next() {
		var instanceName string
		var item VMNetworkInfo
		if scanErr := rows.Scan(
			&instanceName,
			&item.NetworkName,
			&item.MACAddress,
			&item.IPAddress,
			&item.NetworkType,
			&item.TrafficType,
			&item.Gateway,
			&item.CIDR,
			&item.BroadcastURI,
			&item.IsDefault,
			&item.NetworkOffering,
		); scanErr != nil {
			log.Printf("DB scan failed for vm network map: %v", scanErr)
			continue
		}
		if instanceName == "" {
			continue
		}
		temp[instanceName] = append(temp[instanceName], item)
	}

	vmNetworkMapLock.Lock()
	vmNetworkMap = temp
	vmNetworkLastLoad = time.Now()
	vmNetworkMapLock.Unlock()
}

func buildVMNetworkMapQuery(db *sql.DB) string {
	vmColumns := moldTableColumns(db, "vm_instance")
	nicColumns := moldTableColumns(db, "nics")
	networkColumns := moldTableColumns(db, "networks")
	if len(vmColumns) == 0 || len(nicColumns) == 0 {
		return ""
	}

	offeringTable := ""
	offeringColumns := map[string]bool{}
	for _, table := range []string{"network_offerings", "network_offering"} {
		columns := moldTableColumns(db, table)
		if len(columns) > 0 {
			offeringTable = table
			offeringColumns = columns
			break
		}
	}

	joins := []string{"JOIN nics n ON n.instance_id = v.id"}
	if len(networkColumns) > 0 && nicColumns["network_id"] {
		joins = append(joins, "LEFT JOIN networks net ON net.id = n.network_id")
	}
	if offeringTable != "" && networkColumns["network_offering_id"] {
		joins = append(joins, fmt.Sprintf("LEFT JOIN %s no ON no.id = net.network_offering_id", offeringTable))
	}

	where := []string{}
	if vmColumns["removed"] {
		where = append(where, "v.removed IS NULL")
	}
	if nicColumns["removed"] {
		where = append(where, "n.removed IS NULL")
	}
	if networkColumns["removed"] {
		where = append(where, "(net.removed IS NULL OR net.id IS NULL)")
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, "\n\t\t  AND ")
	}

	instanceNameExpr := moldCoalesceExpression("v", vmColumns, []string{"instance_name", "name"}, "''")
	networkNameExpr := moldCoalesceExpression("net", networkColumns, []string{"name", "display_text"}, "''")
	macExpr := moldCoalesceExpression("n", nicColumns, []string{"mac_address", "mac"}, "''")
	ipExpr := moldCoalesceExpression("n", nicColumns, []string{"ip4_address", "ip_address", "ipaddress"}, "''")
	networkTypeExpr := moldCoalesceExpression("net", networkColumns, []string{"guest_type", "network_type", "type"}, "''")
	trafficTypeExpr := moldCoalesceExpression("net", networkColumns, []string{"traffic_type"}, "''")
	gatewayExpr := moldCoalesceExpression("n", nicColumns, []string{"gateway"}, moldCoalesceExpression("net", networkColumns, []string{"gateway"}, "''"))
	cidrExpr := moldCoalesceExpression("net", networkColumns, []string{"cidr", "network_cidr"}, moldCoalesceExpression("n", nicColumns, []string{"netmask"}, "''"))
	broadcastURIExpr := moldCoalesceExpression("net", networkColumns, []string{"broadcast_uri", "broadcast_domain_type", "vlan_id"}, "''")
	isDefaultExpr := moldCoalesceExpression("n", nicColumns, []string{"default_nic", "is_default", "primary_nic"}, "''")
	networkOfferingExpr := moldCoalesceExpression("no", offeringColumns, []string{"name", "display_text", "unique_name"}, "''")

	orderExpr := "v.id DESC"
	if nicColumns["id"] {
		orderExpr += ", n.id DESC"
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
		       %s,
		       %s
		FROM vm_instance v
		%s
		%s
		ORDER BY %s`,
		instanceNameExpr,
		networkNameExpr,
		macExpr,
		ipExpr,
		networkTypeExpr,
		trafficTypeExpr,
		gatewayExpr,
		cidrExpr,
		broadcastURIExpr,
		isDefaultExpr,
		networkOfferingExpr,
		strings.Join(joins, "\n\t\t"),
		whereClause,
		orderExpr,
	)
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

func GetVMNetworkMapLastLoad() time.Time {
	vmNetworkMapLock.RLock()
	defer vmNetworkMapLock.RUnlock()
	return vmNetworkLastLoad
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
