package libvirt

import (
	"encoding/xml"
	"math"
	"reflect"
	"testing"
)

func TestDomainResourceMetadata(t *testing.T) {
	tests := []struct {
		name string
		xml  string
		want map[string]interface{}
	}{
		{"local system VM", `<domain><vcpu>4</vcpu><memory>8388608</memory></domain>`, map[string]interface{}{"CpuNumber": int64(4), "Memory": float64(8192)}},
		{"live allocations", `<domain><vcpu current="2">8</vcpu><memory unit="GiB">8</memory><currentMemory unit="MiB">4096</currentMemory></domain>`, map[string]interface{}{"CpuNumber": int64(2), "Memory": float64(4096)}},
		{"missing allocations", `<domain><uuid>uuid-only</uuid></domain>`, map[string]interface{}{}},
		{"invalid allocations", `<domain><vcpu>-1</vcpu><memory unit="unknown">4096</memory></domain>`, map[string]interface{}{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Verify embedding works as it does in the collector's Domain struct.
			var domain struct{ DomainResources }
			if err := xml.Unmarshal([]byte(test.xml), &domain); err != nil {
				t.Fatal(err)
			}
			if got := domain.metadata(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("metadata = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestDomainMemoryUnits(t *testing.T) {
	for unit, want := range map[string]float64{
		"": 1, "KiB": 1, "k": 1, "bytes": 1.0 / 1024, "KB": 1000.0 / 1024,
		"MiB": 1024, "M": 1024, "MB": 1e6 / 1024,
		"GiB": 1024 * 1024, "G": 1024 * 1024, "GB": 1e9 / 1024,
		"TiB": 1024 * 1024 * 1024, "T": 1024 * 1024 * 1024, "TB": 1e12 / 1024,
	} {
		if got := (domainMemory{Value: 1024, Unit: unit}).mib(); math.Abs(got-want) > 0.000001 {
			t.Errorf("unit %q: got %v MiB, want %v", unit, got, want)
		}
	}
}
