package server

import (
	"fmt"
	"regexp"
	"strings"
)

var wallWindowsDrive = regexp.MustCompile(`(?i)^[a-z]:[\\/]*$`)

// Select one root filesystem, not the first/longest series among all guest
// partitions. Reserved partitions and optical drives must never become usage.
func pickWallVMRootFilesystem(total, metadata *prometheusQueryRangeResponse) ([]wallHostTrendPoint, map[string]interface{}) {
	if total == nil {
		return []wallHostTrendPoint{}, nil
	}
	linux := *total
	linux.Data.Result = nil
	for _, item := range total.Data.Result {
		if item.Metric["partition_mountpoint"] == "/" {
			linux.Data.Result = append(linux.Data.Result, item)
		}
	}
	if len(linux.Data.Result) > 0 {
		return pickPrometheusSeries(&linux)
	}
	if metadata == nil {
		return []wallHostTrendPoint{}, nil
	}
	windows := *total
	windows.Data.Result = nil
	for _, item := range total.Data.Result {
		mount := fmt.Sprint(item.Metric["partition_mountpoint"])
		fsType := strings.ToLower(fmt.Sprint(item.Metric["partition_type"]))
		serial := fmt.Sprint(item.Metric["serial"])
		if !wallWindowsDrive.MatchString(mount) || serial == "" || serial == "<nil>" || fsType == "cdfs" || fsType == "udf" || fsType == "iso9660" {
			continue
		}
		var root map[string]interface{}
		ambiguous := false
		for _, disk := range metadata.Data.Result {
			match := true
			for _, key := range []string{"domain", "instance", "job", "serial"} {
				if fmt.Sprint(item.Metric[key]) != fmt.Sprint(disk.Metric[key]) {
					match = false
					break
				}
			}
			if !match || !wallFilesystemMetadataPresent(disk.Values) {
				continue
			}
			if !strings.EqualFold(fmt.Sprint(disk.Metric["mold_volume_type"]), "ROOT") {
				ambiguous = true
			} else {
				root = disk.Metric
			}
		}
		if root == nil || ambiguous {
			continue
		}
		labels := make(map[string]interface{}, len(item.Metric)+2)
		for key, value := range item.Metric {
			labels[key] = value
		}
		labels["mold_volume_type"] = "ROOT"
		if value, ok := root["display_volume_name"]; ok {
			labels["display_volume_name"] = value
		}
		item.Metric = labels
		windows.Data.Result = append(windows.Data.Result, item)
	}
	// Prefer the currently reporting exporter after migration. If multiple
	// different ROOT drive letters remain equally current, leave usage missing.
	latest := int64(-1)
	selected := -1
	identity := ""
	ambiguous := false
	for i, item := range windows.Data.Result {
		candidate := windows
		candidate.Data.Result = windows.Data.Result[i : i+1]
		points, _ := pickPrometheusSeries(&candidate)
		value := lastTrendValue(points)
		if value == nil || *value <= 0 {
			continue
		}
		stamp := int64(-1)
		for _, point := range points {
			if point.Value != nil && point.Timestamp > stamp {
				stamp = point.Timestamp
			}
		}
		key := fmt.Sprint(item.Metric["domain"], "|", item.Metric["serial"], "|", item.Metric["partition_mountpoint"], "|", item.Metric["partition_name"])
		if stamp > latest {
			latest, selected, identity, ambiguous = stamp, i, key, false
		} else if stamp == latest && key != identity {
			ambiguous = true
		}
	}
	if selected < 0 || ambiguous {
		return []wallHostTrendPoint{}, nil
	}
	windows.Data.Result = windows.Data.Result[selected : selected+1]
	return pickPrometheusSeries(&windows)
}

func wallFilesystemMetadataPresent(values [][]interface{}) bool {
	for i := len(values) - 1; i >= 0; i-- {
		if len(values[i]) < 2 {
			continue
		}
		if value := parsePrometheusFloat(values[i][1]); value != nil && *value > 0 {
			return true
		}
	}
	return false
}
