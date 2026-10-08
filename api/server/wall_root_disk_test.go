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

func TestWallRootDiskSelectors(t *testing.T) {
	for _, key := range []string{"rootDiskTotal", "rootDiskUsed", "rootDiskAvailable"} {
		host := wallHostTrendQuery(key, "10.10.31.1:3003", "cube")
		for _, selector := range []string{`instance="10.10.31.1:3003"`, `job="cube"`, `mountpoint="/"`, `fstype!="rootfs"`} {
			if !strings.Contains(host, selector) {
				t.Fatalf("host root selector missing: %s", host)
			}
		}
		vm := wallVMTrendQuery(key, "i-2-42-VM|s-173-VM")
		if !strings.Contains(vm, `partition_mountpoint="/"`) || !strings.Contains(vm, `domain=~"i-2-42-VM|s-173-VM"`) {
			t.Fatalf("VM root selector missing: %s", vm)
		}
		if strings.Contains(vm, "block_meta") {
			t.Fatal("root filesystem availability must not depend on Mold volume metadata")
		}
	}
}

func TestWallRootDiskHostFallbackAndMeasuredZero(t *testing.T) {
	prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		result := []interface{}{}
		if strings.Contains(query, `instance="cube-1:3003"`) {
			value := "0"
			if !strings.Contains(query, " - ") {
				value = "21474836480"
			}
			result = append(result, map[string]interface{}{"metric": map[string]string{"instance": "cube-1:3003", "mountpoint": "/"},
				"values": [][]interface{}{{1700000000, value}}})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "data": map[string]interface{}{"resultType": "matrix", "result": result}})
	}))
	defer prometheus.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	items := <-startWallRootDiskQueries(ctx, prometheus.Client(), prometheus.URL, []string{"10.10.31.1:3003", "cube-1:3003"}, "", "cube", time.Now().Add(-time.Hour), time.Now(), time.Minute)
	if len(items) != 3 || items[0].LastValue == nil || *items[0].LastValue != 21474836480 || items[1].LastValue == nil || *items[1].LastValue != 0 {
		t.Fatalf("unexpected root disk measurements: %#v", items)
	}
	for _, item := range items {
		if item.Labels["instance"] != "cube-1:3003" || item.Unit != "bytes" {
			t.Fatalf("mixed exporters or units: %#v", item)
		}
	}
}

func TestWallRootDiskCancellationReturnsMissingNotZero(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items := <-startWallRootDiskQueries(ctx, &http.Client{}, "http://127.0.0.1:1", nil, "i-2-42-VM", "", time.Now(), time.Now(), time.Minute)
	for _, item := range items {
		if item.LastValue != nil || len(item.Values) != 0 {
			t.Fatalf("failed query must not fabricate usage: %#v", item)
		}
	}
}

func TestWallRootDiskMatchesFilesystemAcrossExporters(t *testing.T) {
	var response prometheusQueryRangeResponse
	if err := json.Unmarshal([]byte(`{"status":"success","data":{"result":[
		{"metric":{"domain":"i-2-42-VM","instance":"old-host","partition_mountpoint":"/","serial":"old"},"values":[[1700000000,"999"],[1700000060,"888"]]},
		{"metric":{"domain":"i-2-42-VM","instance":"new-host","partition_mountpoint":"/","serial":"new"},"values":[[1700000060,"0"]]}
	]}}`), &response); err != nil {
		t.Fatal(err)
	}
	labels := map[string]interface{}{"domain": "i-2-42-VM", "instance": "new-host", "partition_mountpoint": "/", "serial": "new"}
	points, _ := pickWallRootDiskSeries(&response, labels)
	last := lastTrendValue(points)
	if last == nil || *last != 0 {
		t.Fatalf("mixed filesystem measurements: %#v", points)
	}
	labels["serial"] = "missing"
	points, _ = pickWallRootDiskSeries(&response, labels)
	if len(points) != 0 {
		t.Fatal("missing matching filesystem must remain uncollected")
	}
}
