package common

import (
	"encoding/json"
	"strings"
	"testing"
)

func networkColumns(names ...string) map[string]bool {
	columns := map[string]bool{}
	for _, name := range names {
		columns[name] = true
	}
	return columns
}

func TestVMNetworkIdentityQuery(t *testing.T) {
	query := buildVMNetworkMapSQL(
		networkColumns("id", "uuid", "instance_name", "removed", "data_center_id"),
		networkColumns("id", "uuid", "instance_id", "network_id", "mac_address", "ip4_address", "removed", "netmask"),
		networkColumns("id", "uuid", "name", "guest_type", "cidr", "broadcast_uri", "data_center_id", "removed"),
		nil, networkColumns("id", "uuid", "name"), "")
	for _, clause := range []string{"v.uuid", "n.mac_address", "net.uuid", "n.uuid", "zone.uuid", "zone.name", "net.cidr", "net.id = n.network_id", "zone.id = net.data_center_id", "n.removed IS NULL"} {
		if !strings.Contains(query, clause) {
			t.Errorf("identity query missing %s", clause)
		}
	}
	if strings.Contains(query, "n.netmask") {
		t.Fatal("netmask must not masquerade as a CIDR")
	}
}

func TestVMNetworkOptionalSchema(t *testing.T) {
	query := buildVMNetworkMapSQL(networkColumns("id", "uuid", "name", "data_center_id"), networkColumns("instance_id", "mac_address"),
		networkColumns("id", "uuid", "network_offering_id"), networkColumns("name"), networkColumns("id", "name"), "network_offerings")
	if strings.Contains(query, "net.") || strings.Contains(query, "no.") {
		t.Fatal("query references an alias without a join")
	}
	if !strings.Contains(query, "zone.id = v.data_center_id") {
		t.Fatal("VM zone fallback missing")
	}
}

func TestVMNetworkIdentityPayload(t *testing.T) {
	value := VMNetworkInfo{VMUUID: "vm-uuid", NetworkID: "network-uuid", NICID: "nic-uuid", VLAN: "153", ZoneID: "zone-uuid", ZoneName: "zone-a"}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]string
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{"vmUuid": "vm-uuid", "networkId": "network-uuid", "nicId": "nic-uuid", "vlan": "153", "zoneId": "zone-uuid", "zoneName": "zone-a"} {
		if fields[key] != expected {
			t.Errorf("%s = %q", key, fields[key])
		}
	}
}
