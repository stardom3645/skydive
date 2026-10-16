package common

import (
	"database/sql"
	"fmt"
	"net"
	"sort"
	"strings"
)

// Query only VM bindings. Sharing an account, network or source NAT gateway
// does not make a public address belong to a VM.
func moldVMPublicIPQuery(tables map[string]map[string]bool) string {
	vm, ip, pf, fw := tables["vm_instance"], tables["user_ip_address"], tables["port_forwarding_rules"], tables["firewall_rules"]
	if !vm["id"] || !vm["uuid"] || !ip["id"] || !ip["public_ip_address"] || !ip["state"] {
		return ""
	}
	active := "LOWER(TRIM(ip.state)) = 'allocated'"
	if vm["removed"] {
		active += " AND v.removed IS NULL"
	}
	if ip["removed"] {
		active += " AND ip.removed IS NULL"
	}
	if ip["account_id"] && vm["account_id"] {
		active += " AND ip.account_id = v.account_id"
	}
	queries := []string{}
	if ip["one_to_one_nat"] {
		for _, column := range []string{"vm_id", "virtual_machine_id"} {
			if ip[column] {
				queries = append(queries, fmt.Sprintf("SELECT v.uuid, ip.public_ip_address FROM user_ip_address ip JOIN vm_instance v ON v.id = ip.%s WHERE ip.one_to_one_nat = 1 AND %s", column, active))
				break
			}
		}
	}
	if pf["id"] && pf["instance_id"] && fw["id"] && fw["ip_address_id"] && fw["state"] && fw["purpose"] {
		where := active + " AND LOWER(TRIM(fr.state)) = 'active' AND LOWER(TRIM(fr.purpose)) = 'portforwarding'"
		if fw["account_id"] && vm["account_id"] {
			where += " AND fr.account_id = v.account_id"
		}
		if fw["removed"] {
			where += " AND fr.removed IS NULL"
		}
		if pf["removed"] {
			where += " AND pf.removed IS NULL"
		}
		queries = append(queries, "SELECT v.uuid, ip.public_ip_address FROM port_forwarding_rules pf JOIN firewall_rules fr ON fr.id = pf.id JOIN user_ip_address ip ON ip.id = fr.ip_address_id JOIN vm_instance v ON v.id = pf.instance_id WHERE "+where)
	}
	return strings.Join(queries, " UNION ")
}

func loadMoldVMPublicIPs(db *sql.DB) (map[string][]string, error) {
	tables := map[string]map[string]bool{}
	for _, table := range []string{"vm_instance", "user_ip_address", "port_forwarding_rules", "firewall_rules"} {
		tables[table] = moldTableColumns(db, table)
	}
	query := moldVMPublicIPQuery(tables)
	if query == "" {
		return map[string][]string{}, nil
	}
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return readMoldVMPublicIPs(rows)
}

func readMoldVMPublicIPs(rows *sql.Rows) (map[string][]string, error) {
	seen := map[string]map[string]bool{}
	for rows.Next() {
		var uuid, address sql.NullString
		if err := rows.Scan(&uuid, &address); err != nil {
			return nil, err
		}
		key := strings.ToLower(strings.TrimSpace(uuid.String))
		parsed := net.ParseIP(strings.TrimSpace(address.String))
		if key == "" || parsed == nil {
			continue
		}
		if seen[key] == nil {
			seen[key] = map[string]bool{}
		}
		seen[key][parsed.String()] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := map[string][]string{}
	for uuid, addresses := range seen {
		for address := range addresses {
			result[uuid] = append(result[uuid], address)
		}
		sort.Strings(result[uuid])
	}
	return result, nil
}
