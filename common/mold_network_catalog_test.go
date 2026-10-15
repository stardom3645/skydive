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

type catalogConnection struct{ legacy bool }
type catalogRows struct {
	columns []string
	values  [][]driver.Value
	next    int
}

func (catalogDriver) Open(name string) (driver.Conn, error) {
	return catalogConnection{legacy: name == "legacy"}, nil
}
func (catalogConnection) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not used") }
func (catalogConnection) Close() error                        { return nil }
func (catalogConnection) Begin() (driver.Tx, error)           { return nil, errors.New("not used") }
func (connection catalogConnection) Query(query string, _ []driver.Value) (driver.Rows, error) {
	if strings.HasPrefix(query, "SHOW COLUMNS") {
		columns := []string{"id", "uuid", "name"}
		if strings.HasSuffix(query, "networks") {
			columns = append(columns, "guest_type", "cidr", "broadcast_uri", "data_center_id", "removed")
			if !connection.legacy {
				columns = append(columns, "created", "display_text")
			}
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
	created := "2026-01-01 12:00:00"
	if connection.legacy {
		created = ""
		if strings.Contains(query, "net.created") || strings.Contains(query, "net.display_text") {
			return nil, errors.New("optional columns referenced in legacy schema")
		}
	} else if !strings.Contains(query, "CAST(net.created AS CHAR)") || !strings.Contains(query, "NULLIF(TRIM(net.name), '')") || !strings.Contains(query, "NULLIF(TRIM(net.display_text), '')") {
		return nil, errors.New("actual creation date and nonempty name fallback required")
	}
	return &catalogRows{columns: []string{"id", "name", "type", "cidr", "uri", "vlan", "zoneId", "zoneName", "created"}, values: [][]driver.Value{
		{"network-a", "same-name", "Isolated", "10.0.0.0/24", "vlan://153", "", "zone-a", "Zone A", created},
		{"network-b", "same-name", "Shared", "10.0.0.0/24", "vlan://153", "", "zone-b", "Zone B", ""},
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
	if networks[0].NetworkCreatedAt != "2026-01-01 12:00:00" || networks[1].NetworkCreatedAt != "" {
		t.Fatal("creation dates must preserve actual and missing DB values")
	}
}

func TestMoldNetworkCatalogWithoutCreationColumn(t *testing.T) {
	catalogDriverOnce.Do(func() { sql.Register("mold-network-catalog-test", catalogDriver{}) })
	db, err := sql.Open("mold-network-catalog-test", "legacy")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	networks, err := loadMoldNetworkCatalog(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(networks) != 2 || networks[0].NetworkCreatedAt != "" || networks[1].NetworkCreatedAt != "" {
		t.Fatal("legacy schemas must retain networks without invented dates")
	}
}
