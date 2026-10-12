package common

import (
	"database/sql"
	"fmt"
)

var moldNetworkCatalog = []VMNetworkInfo{}

// Reuse the same DB/schema introspection as the VM NIC cache, including
// networks without NICs so the selector does not depend on VM discovery.
func loadMoldNetworkCatalog(db *sql.DB) ([]VMNetworkInfo, error) {
	columns := moldTableColumns(db, "networks")
	zones := moldTableColumns(db, "data_center")
	if !columns["id"] {
		return []VMNetworkInfo{}, nil
	}
	join := ""
	if columns["data_center_id"] && zones["id"] {
		join = "LEFT JOIN data_center zone ON zone.id = net.data_center_id"
	} else {
		zones = map[string]bool{}
	}
	where := ""
	if columns["removed"] {
		where = "WHERE net.removed IS NULL"
	}
	expr := func(names ...string) string { return moldCoalesceExpression("net", columns, names, "''") }
	query := fmt.Sprintf("SELECT %s, %s, %s, %s, %s, %s, %s, %s FROM networks net %s %s ORDER BY net.id",
		expr("uuid", "id"), expr("name", "display_text"), expr("guest_type", "network_type", "type"), expr("cidr", "network_cidr"),
		expr("broadcast_uri"), expr("vlan_id"), moldCoalesceExpression("zone", zones, []string{"uuid", "id"}, "''"),
		moldCoalesceExpression("zone", zones, []string{"name"}, "''"), join, where)
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []VMNetworkInfo{}
	for rows.Next() {
		var item VMNetworkInfo
		if err := rows.Scan(&item.NetworkID, &item.NetworkName, &item.NetworkType, &item.CIDR, &item.BroadcastURI, &item.VLAN, &item.ZoneID, &item.ZoneName); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func GetMoldNetworkCatalog() []VMNetworkInfo {
	vmNetworkMapLock.RLock()
	defer vmNetworkMapLock.RUnlock()
	result := make([]VMNetworkInfo, len(moldNetworkCatalog))
	copy(result, moldNetworkCatalog)
	return result
}
