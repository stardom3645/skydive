package common

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

type catalogDriver struct{}

var catalogDriverOnce sync.Once

type catalogConnection struct{}
type catalogRows struct {
	columns []string
	values  [][]driver.Value
	next    int
}

func (catalogDriver) Open(string) (driver.Conn, error)        { return catalogConnection{}, nil }
func (catalogConnection) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (catalogConnection) Close() error                        { return nil }
func (catalogConnection) Begin() (driver.Tx, error)           { return nil, errors.New("not used") }
func (catalogConnection) Query(query string, _ []driver.Value) (driver.Rows, error) {
	if strings.HasPrefix(query, "SHOW COLUMNS") {
		columns := []string{"id", "uuid", "name"}
		if strings.HasSuffix(query, "networks") {
			columns = append(columns, "guest_type", "cidr", "broadcast_uri", "data_center_id", "removed")
		}
		rows := &catalogRows{columns: []string{"Field", "Type", "Null", "Key", "Default", "Extra"}}
		for _, name := range columns {
			rows.values = append(rows.values, []driver.Value{name, "varchar", "YES", "", nil, ""})
		}
		return rows, nil
	}
	if strings.Contains(query, "nics") || !strings.Contains(query, "net.removed IS NULL") || !strings.Contains(query, "zone.id = net.data_center_id") {
		return nil, errors.New("catalog must include unattached active networks using the Zone join")
	}
	return &catalogRows{columns: []string{"id", "name", "type", "cidr", "uri", "vlan", "zoneId", "zoneName"}, values: [][]driver.Value{
		{"network-a", "same-name", "Isolated", "10.0.0.0/24", "vlan://153", "", "zone-a", "Zone A"},
		{"network-b", "same-name", "Shared", "10.0.0.0/24", "vlan://153", "", "zone-b", "Zone B"},
	}}, nil
}
func (rows *catalogRows) Columns() []string { return rows.columns }
func (rows *catalogRows) Close() error      { return nil }
func (rows *catalogRows) Next(dest []driver.Value) error {
	if rows.next >= len(rows.values) {
		return io.EOF
	}
	copy(dest, rows.values[rows.next])
	rows.next++
	return nil
}

func TestMoldNetworkCatalogIncludesUnattachedDistinctNetworks(t *testing.T) {
	catalogDriverOnce.Do(func() { sql.Register("mold-network-catalog-test", catalogDriver{}) })
	db, err := sql.Open("mold-network-catalog-test", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	networks, err := loadMoldNetworkCatalog(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(networks) != 2 || networks[0].NetworkID == networks[1].NetworkID {
		t.Fatal("equal names, CIDRs and VLANs must retain different network IDs")
	}
	if networks[0].ZoneName != "Zone A" || networks[1].NetworkType != "Shared" {
		t.Fatal("network metadata lost")
	}
}
