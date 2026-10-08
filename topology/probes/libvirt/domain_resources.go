package libvirt

import (
	"math"
	"strings"
)

// DomainResources describes allocations in live domain XML, not utilization.
// Keep memory in MiB to match Mold VM metadata and the existing VM detail UI.
type DomainResources struct {
	VCPU struct {
		Count   int64 `xml:",chardata"`
		Current int64 `xml:"current,attr"`
	} `xml:"vcpu"`
	Memory        domainMemory `xml:"memory"`
	CurrentMemory domainMemory `xml:"currentMemory"`
}

type domainMemory struct {
	Value float64 `xml:",chardata"`
	Unit  string  `xml:"unit,attr"`
}

func (memory domainMemory) mib() float64 {
	if memory.Value <= 0 || math.IsNaN(memory.Value) || math.IsInf(memory.Value, 0) {
		return 0
	}
	var multiplier float64
	switch strings.ToLower(strings.TrimSpace(memory.Unit)) {
	case "b", "bytes":
		multiplier = 1
	case "kb":
		multiplier = 1e3
	case "", "k", "kib":
		multiplier = 1024
	case "mb":
		multiplier = 1e6
	case "m", "mib":
		multiplier = 1024 * 1024
	case "gb":
		multiplier = 1e9
	case "g", "gib":
		multiplier = 1024 * 1024 * 1024
	case "tb":
		multiplier = 1e12
	case "t", "tib":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0
	}
	value := memory.Value * multiplier / (1024 * 1024)
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}

func (resources DomainResources) metadata() map[string]interface{} {
	metadata := make(map[string]interface{})
	cpu := resources.VCPU.Count
	if resources.VCPU.Current > 0 && resources.VCPU.Current <= cpu {
		cpu = resources.VCPU.Current
	}
	if cpu > 0 {
		metadata["CpuNumber"] = cpu
	}
	memory := resources.CurrentMemory.mib()
	if memory == 0 {
		memory = resources.Memory.mib()
	}
	if memory > 0 {
		metadata["Memory"] = memory
	}
	return metadata
}
