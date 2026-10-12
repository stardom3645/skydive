package common

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type VMNetworkInfo struct {
	VMUUID          string `json:"vmUuid"`
	NetworkID       string `json:"networkId"`
	NICID           string `json:"nicId"`
	VLAN            string `json:"vlan"`
	ZoneID          string `json:"zoneId"`
	ZoneName        string `json:"zoneName"`
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
		if !errors.Is(err, ErrMoldDBPasswordNotConfigured) {
			log.Printf("DB connection failed for vm network map: %v", err)
		}
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
			&item.VMUUID, &item.NetworkID, &item.NICID, &item.VLAN, &item.ZoneID, &item.ZoneName,
		); scanErr != nil {
			log.Printf("DB scan failed for vm network map: %v", scanErr)
			return
		}
		if instanceName == "" {
			continue
		}
		temp[instanceName] = append(temp[instanceName], item)
	}

	if err := rows.Err(); err != nil {
		log.Printf("DB read failed for vm network map: %v", err)
		return
	}

	catalog, err := loadMoldNetworkCatalog(db)
	if err != nil {
		log.Printf("DB read failed for Mold networks: %v", err)
		return
	}
	vmNetworkMapLock.Lock()
	vmNetworkMap = temp
	moldNetworkCatalog = catalog
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

	zoneColumns := moldTableColumns(db, "data_center")
	return buildVMNetworkMapSQL(vmColumns, nicColumns, networkColumns, offeringColumns, zoneColumns, offeringTable)
}

func buildVMNetworkMapSQL(vmColumns, nicColumns, networkColumns, offeringColumns, zoneColumns map[string]bool, offeringTable string) string {
	if !vmColumns["id"] || !nicColumns["instance_id"] {
		return ""
	}
	if !nicColumns["network_id"] || !networkColumns["id"] {
		networkColumns = map[string]bool{}
	}
	if !networkColumns["network_offering_id"] || offeringTable == "" {
		offeringColumns = map[string]bool{}
		offeringTable = ""
	}
	joins := []string{"JOIN nics n ON n.instance_id = v.id"}
	if len(networkColumns) > 0 && nicColumns["network_id"] {
		joins = append(joins, "LEFT JOIN networks net ON net.id = n.network_id")
	}
	if offeringTable != "" && networkColumns["network_offering_id"] {
		joins = append(joins, fmt.Sprintf("LEFT JOIN %s no ON no.id = net.network_offering_id", offeringTable))
	}

	zoneSource := ""
	if len(networkColumns) > 0 && nicColumns["network_id"] && networkColumns["data_center_id"] {
		zoneSource = "net.data_center_id"
	} else if vmColumns["data_center_id"] {
		zoneSource = "v.data_center_id"
	}
	if zoneSource != "" && zoneColumns["id"] {
		joins = append(joins, "LEFT JOIN data_center zone ON zone.id = "+zoneSource)
	} else {
		zoneColumns = map[string]bool{}
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
	cidrExpr := moldCoalesceExpression("net", networkColumns, []string{"cidr", "network_cidr"}, "''")
	broadcastURIExpr := moldCoalesceExpression("net", networkColumns, []string{"broadcast_uri", "broadcast_domain_type", "vlan_id"}, "''")
	isDefaultExpr := moldCoalesceExpression("n", nicColumns, []string{"default_nic", "is_default", "primary_nic"}, "''")
	networkOfferingExpr := moldCoalesceExpression("no", offeringColumns, []string{"name", "display_text", "unique_name"}, "''")

	vmUUIDExpr := moldCoalesceExpression("v", vmColumns, []string{"uuid"}, "''")
	networkIDExpr := moldCoalesceExpression("net", networkColumns, []string{"uuid", "id"}, "''")
	nicIDExpr := moldCoalesceExpression("n", nicColumns, []string{"uuid", "id"}, "''")
	vlanExpr := moldCoalesceExpression("net", networkColumns, []string{"vlan_id"}, "''")
	zoneIDExpr := moldCoalesceExpression("zone", zoneColumns, []string{"uuid", "id"}, "''")
	zoneNameExpr := moldCoalesceExpression("zone", zoneColumns, []string{"name"}, "''")
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
		       %s,
		       %s, %s, %s, %s, %s, %s
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
		vmUUIDExpr, networkIDExpr, nicIDExpr, vlanExpr, zoneIDExpr, zoneNameExpr,
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
