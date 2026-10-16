package common

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func publicIPSchema() map[string]map[string]bool {
	return map[string]map[string]bool{
		"vm_instance":           {"id": true, "uuid": true, "removed": true, "account_id": true},
		"user_ip_address":       {"id": true, "public_ip_address": true, "state": true, "one_to_one_nat": true, "vm_id": true, "account_id": true},
		"port_forwarding_rules": {"id": true, "instance_id": true},
		"firewall_rules":        {"id": true, "ip_address_id": true, "state": true, "purpose": true, "account_id": true},
	}
}

func TestVMPublicIPBindingQuery(t *testing.T) {
	schema := publicIPSchema()
	query := moldVMPublicIPQuery(schema)
	for _, required := range []string{
		"v.id = ip.vm_id", "ip.one_to_one_nat = 1", "v.id = pf.instance_id", "fr.id = pf.id", "ip.id = fr.ip_address_id",
		"LOWER(TRIM(fr.state)) = 'active'", "LOWER(TRIM(fr.purpose)) = 'portforwarding'", "LOWER(TRIM(ip.state)) = 'allocated'",
		"v.removed IS NULL", "ip.account_id = v.account_id", "fr.account_id = v.account_id", " UNION ",
	} {
		if !strings.Contains(query, required) {
			t.Errorf("missing binding guard %q in %s", required, query)
		}
	}
	if strings.Contains(query, "source_nat") || strings.Contains(query, "network_id =") {
		t.Fatal("a shared source NAT/network is not a VM binding")
	}
	delete(schema["user_ip_address"], "vm_id")
	schema["user_ip_address"]["virtual_machine_id"] = true
	schema["user_ip_address"]["removed"] = true
	schema["firewall_rules"]["removed"] = true
	schema["port_forwarding_rules"]["removed"] = true
	query = moldVMPublicIPQuery(schema)
	for _, required := range []string{"v.id = ip.virtual_machine_id", "ip.removed IS NULL", "fr.removed IS NULL", "pf.removed IS NULL"} {
		if !strings.Contains(query, required) {
			t.Errorf("missing schema guard %q", required)
		}
	}
}

func TestVMPublicIPUnsupportedSchemas(t *testing.T) {
	schema := publicIPSchema()
	delete(schema, "port_forwarding_rules")
	query := moldVMPublicIPQuery(schema)
	if strings.Contains(query, "port_forwarding_rules") || !strings.Contains(query, "one_to_one_nat") {
		t.Fatal(query)
	}
	delete(schema["user_ip_address"], "one_to_one_nat")
	if moldVMPublicIPQuery(schema) != "" {
		t.Fatal("must omit unverified relationships")
	}
	schema = publicIPSchema()
	delete(schema["user_ip_address"], "state")
	if moldVMPublicIPQuery(schema) != "" {
		t.Fatal("must not show IPs whose allocation state is unknown")
	}
}

type publicIPDriver struct{}
type publicIPConnection struct{ fail bool }

var publicIPDriverOnce sync.Once

func (publicIPDriver) Open(name string) (driver.Conn, error) {
	return publicIPConnection{fail: name == "failed"}, nil
}
func (publicIPConnection) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (publicIPConnection) Close() error                        { return nil }
func (publicIPConnection) Begin() (driver.Tx, error)           { return nil, errors.New("not used") }
func (connection publicIPConnection) Query(query string, _ []driver.Value) (driver.Rows, error) {
	if strings.HasPrefix(query, "SHOW COLUMNS FROM ") {
		table := strings.TrimPrefix(query, "SHOW COLUMNS FROM ")
		rows := &catalogRows{columns: []string{"Field", "Type", "Null", "Key", "Default", "Extra"}}
		for column := range publicIPSchema()[table] {
			rows.values = append(rows.values, []driver.Value{column, "varchar", "YES", "", nil, ""})
		}
		return rows, nil
	}
	if connection.fail {
		return nil, errors.New("bindings unavailable")
	}
	if query != moldVMPublicIPQuery(publicIPSchema()) {
		return nil, errors.New("unexpected VM binding query")
	}
	return &catalogRows{columns: []string{"uuid", "public_ip_address"}, values: [][]driver.Value{
		{"VM-1", "192.0.2.2"}, {"vm-1", "192.0.2.1"}, {"vm-1", "192.0.2.2"},
		{"vm-2", "198.51.100.3"}, {"vm-1", "2001:db8::1"}, {"vm-1", "invalid-ip"}, {nil, "192.0.2.5"}, {"vm-1", nil},
	}}, nil
}

func TestLoadVMPublicIPBindings(t *testing.T) {
	publicIPDriverOnce.Do(func() { sql.Register("vm-public-ip-test", publicIPDriver{}) })
	db, err := sql.Open("vm-public-ip-test", "normal")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	result, err := loadMoldVMPublicIPs(db)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string][]string{"vm-1": {"192.0.2.1", "192.0.2.2", "2001:db8::1"}, "vm-2": {"198.51.100.3"}}
	if !reflect.DeepEqual(result, expected) {
		t.Fatalf("got %#v", result)
	}
	failed, _ := sql.Open("vm-public-ip-test", "failed")
	defer failed.Close()
	if result, err = loadMoldVMPublicIPs(failed); err == nil || result != nil {
		t.Fatal("failed lookups must not return guessed/stale addresses")
	}
}

func TestVMDetailPublicIPJSON(t *testing.T) {
	empty, _ := json.Marshal(VMDetailInfo{UUID: "vm-1"})
	if strings.Contains(string(empty), "publicIps") {
		t.Fatal("omit absent bindings")
	}
	content, _ := json.Marshal(VMDetailInfo{UUID: "vm-1", PublicIPs: []string{"192.0.2.1", "192.0.2.2"}})
	if !strings.Contains(string(content), `"publicIps":["192.0.2.1","192.0.2.2"]`) {
		t.Fatal(string(content))
	}
}
