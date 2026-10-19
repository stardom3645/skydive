package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func wallFSSeries(mount, fsType, partition, serial string, value float64) map[string]interface{} {
	return map[string]interface{}{"metric": map[string]string{"domain": "i-2-73-VM", "instance": "host:3002", "job": "libvirt",
		"partition_mountpoint": mount, "partition_type": fsType, "partition_name": partition, "serial": serial},
		"values": [][]interface{}{{1700000000, value}}}
}

func wallFSMetadata(domain, instance, serial, volumeType string) map[string]interface{} {
	return map[string]interface{}{"metric": map[string]string{"domain": domain, "instance": instance, "job": "libvirt",
		"serial": serial, "mold_volume_type": volumeType, "display_volume_name": "sda ( ROOT-73 )"},
		"values": [][]interface{}{{1700000000, 1}}}
}

func wallFSResponse(rows ...map[string]interface{}) *prometheusQueryRangeResponse {
	encoded, _ := json.Marshal(map[string]interface{}{"data": map[string]interface{}{"result": rows}})
	var response prometheusQueryRangeResponse
	json.Unmarshal(encoded, &response)
	response.Status = "success"
	return &response
}

func TestWallWindowsRootFilesystem(t *testing.T) {
	total := wallFSResponse(wallFSSeries("System Reserved", "NTFS", "recovery", "root", 914),
		wallFSSeries(`C:\`, "NTFS", "os", "root", 100000), wallFSSeries("System Reserved", "FAT32", "efi", "root", 196),
		wallFSSeries(`D:\`, "CDFS", "cdrom", "", 24), wallFSSeries(`E:\`, "NTFS", "data", "data", 200000))
	metadata := wallFSResponse(wallFSMetadata("i-2-73-VM", "host:3002", "root", "ROOT"),
		wallFSMetadata("i-2-73-VM", "host:3002", "data", "DATADISK"))
	points, labels := pickWallVMRootFilesystem(total, metadata)
	if labels["partition_mountpoint"] != `C:\` || labels["mold_volume_type"] != "ROOT" || *lastTrendValue(points) != 100000 {
		t.Fatalf("selected reserved, data, or optical partition: %#v, %#v", labels, points)
	}
	used := wallFSResponse(wallFSSeries("System Reserved", "NTFS", "recovery", "root", 831), wallFSSeries(`C:\`, "NTFS", "os", "root", 0))
	usage, _ := pickWallRootDiskSeries(used, labels)
	if lastTrendValue(usage) == nil || *lastTrendValue(usage) != 0 {
		t.Fatal("must match the same partition and preserve zero usage")
	}
	if _, labels := pickWallVMRootFilesystem(total, nil); labels != nil {
		t.Fatal("Windows drive letters alone must not imply a ROOT volume")
	}
}

func TestWallWindowsRootIdentityIsolation(t *testing.T) {
	total := wallFSResponse(wallFSSeries(`C:\`, "NTFS", "os", "root", 100000))
	for _, metadata := range []*prometheusQueryRangeResponse{
		wallFSResponse(wallFSMetadata("other-vm", "host:3002", "root", "ROOT")),
		wallFSResponse(wallFSMetadata("i-2-73-VM", "other-host:3002", "root", "ROOT")),
		wallFSResponse(wallFSMetadata("i-2-73-VM", "host:3002", "other-serial", "ROOT")),
		wallFSResponse(wallFSMetadata("i-2-73-VM", "host:3002", "root", "DATADISK")),
		wallFSResponse(wallFSMetadata("i-2-73-VM", "host:3002", "root", "ROOT"), wallFSMetadata("i-2-73-VM", "host:3002", "root", "DATADISK")),
	} {
		if _, labels := pickWallVMRootFilesystem(total, metadata); labels != nil {
			t.Fatalf("unverified ROOT identity accepted: %#v", labels)
		}
	}
	metadata := wallFSResponse(wallFSMetadata("i-2-73-VM", "host:3002", "root", "ROOT"))
	for _, optical := range []string{"CDFS", "UDF", "iso9660"} {
		if _, labels := pickWallVMRootFilesystem(wallFSResponse(wallFSSeries(`C:\`, optical, "os", "root", 100)), metadata); labels != nil {
			t.Fatal("optical filesystem must not become root usage")
		}
	}
	if _, labels := pickWallVMRootFilesystem(nil, metadata); labels != nil {
		t.Fatal("missing Guest Agent data must remain missing")
	}
}

func TestWallWindowsRootDoesNotAssumeCDrive(t *testing.T) {
	metadata := wallFSResponse(wallFSMetadata("i-2-73-VM", "host:3002", "root", "ROOT"), wallFSMetadata("i-2-73-VM", "host:3002", "data", "DATADISK"))
	total := wallFSResponse(wallFSSeries(`C:\`, "NTFS", "data", "data", 200000), wallFSSeries(`F:\`, "NTFS", "os", "root", 100000))
	if _, labels := pickWallVMRootFilesystem(total, metadata); labels["partition_mountpoint"] != `F:\` {
		t.Fatal("must use the verified drive rather than hard-coding C:")
	}
	total.Data.Result = append(total.Data.Result, wallFSResponse(wallFSSeries(`G:\`, "NTFS", "another", "root", 100000)).Data.Result...)
	if _, labels := pickWallVMRootFilesystem(total, metadata); labels != nil {
		t.Fatal("ambiguous ROOT drive letters must remain missing")
	}
	// Linux remains independent of block metadata, including absent mappings.
	linux := wallFSResponse(wallFSSeries("/boot", "ext4", "boot", "root", 1000), wallFSSeries("/", "xfs", "os", "", 100000))
	if _, labels := pickWallVMRootFilesystem(linux, nil); labels["partition_mountpoint"] != "/" {
		t.Fatal("Linux root filesystem regressed")
	}
}

func TestWallWindowsRootAfterMigration(t *testing.T) {
	old := wallFSSeries(`C:\`, "NTFS", "os", "root", 120000)
	old["metric"].(map[string]string)["instance"] = "old-host:3002"
	old["values"] = [][]interface{}{{1699999900, 120000}}
	current := wallFSSeries(`C:\`, "NTFS", "os", "root", 100000)
	metadata := wallFSResponse(wallFSMetadata("i-2-73-VM", "old-host:3002", "root", "ROOT"), wallFSMetadata("i-2-73-VM", "host:3002", "root", "ROOT"))
	points, labels := pickWallVMRootFilesystem(wallFSResponse(old, current), metadata)
	if labels["instance"] != "host:3002" || *lastTrendValue(points) != 100000 {
		t.Fatal("selected the old exporter after migration")
	}
	oldUsage := wallFSSeries(`C:\`, "NTFS", "os", "root", 99999)
	oldUsage["metric"].(map[string]string)["instance"] = "old-host:3002"
	used, _ := pickWallRootDiskSeries(wallFSResponse(oldUsage, wallFSSeries(`C:\`, "NTFS", "os", "root", 0)), labels)
	if *lastTrendValue(used) != 0 {
		t.Fatal("usage came from a different exporter than total capacity")
	}
}

func TestWallWindowsRootQueriesShareFilesystem(t *testing.T) {
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		if strings.Contains(query, `partition_mountpoint="/"`) {
			t.Error("Windows query still restricted to Linux mountpoint")
		}
		var response *prometheusQueryRangeResponse
		switch {
		case strings.Contains(query, "block_meta"):
			response = wallFSResponse(wallFSMetadata("i-2-73-VM", "host:3002", "root", "ROOT"))
		case strings.Contains(query, " - "):
			response = wallFSResponse(wallFSSeries(`C:\`, "NTFS", "os", "root", 100000))
		case strings.Contains(query, "usage_bytes"):
			response = wallFSResponse(wallFSSeries("System Reserved", "NTFS", "reserved", "root", 900), wallFSSeries(`C:\`, "NTFS", "os", "root", 0))
		default:
			response = wallFSResponse(wallFSSeries("System Reserved", "NTFS", "reserved", "root", 1000), wallFSSeries(`C:\`, "NTFS", "os", "root", 100000))
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer prom.Close()
	now := time.Now()
	items := <-startWallRootDiskQueries(context.Background(), prom.Client(), prom.URL, nil, "i-2-73-VM", "", now.Add(-time.Minute), now, time.Minute)
	for i, want := range []float64{100000, 0, 100000} {
		if items[i].LastValue == nil || *items[i].LastValue != want || items[i].Labels["partition_mountpoint"] != `C:\` {
			t.Fatalf("partition mismatch: %#v", items[i])
		}
	}
}
