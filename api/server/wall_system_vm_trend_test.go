package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWallSystemVMExporterIdentity(t *testing.T) {
	now := time.Now()
	for _, scenario := range []struct {
		name, domain, mac, want, job string
		count                        int
		stale, macOnly, failure      bool
	}{
		{name: "exact hostname", domain: "ccvm", want: "10.10.31.10:3003", count: 1},
		{name: "guest MAC selects one storage VM", domain: "scvm", mac: "52:54:00:11:22:33", want: "10.10.1.12:3003", count: 1, macOnly: true},
		{name: "ambiguous hostname", domain: "ccvm", count: 2},
		{name: "ambiguous MAC", domain: "scvm", mac: "52:54:00:11:22:33", count: 2, macOnly: true},
		{name: "missing", domain: "ccvm", count: 0},
		{name: "stale exporter", domain: "ccvm", count: 1, stale: true},
		{name: "Mold domain does not use system job", domain: "i-2-42-VM", count: 1},
		{name: "Mold storage VM exact hostname", domain: "s-173-VM", want: "10.10.31.8:3003", job: "mold-system-vms", count: 1},
		{name: "Mold console VM exact MAC", domain: "v-174-VM", mac: "52:54:00:11:22:33", want: "10.10.31.7:3003", job: "console-guest", count: 1, macOnly: true},
		{name: "Mold storage exporter missing", domain: "s-173-VM", count: 0},
		{name: "Mold console exporter ambiguous", domain: "v-174-VM", job: "console-guest", count: 2},
		{name: "Mold router remains libvirt", domain: "r-192-VM", count: 1},
		{name: "Prometheus failure", domain: "ccvm", failure: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			queries := 0
			prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				queries++
				if scenario.failure {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				query := r.URL.Query().Get("query")
				if scenario.macOnly && (!strings.Contains(query, "node_network_info") || !strings.Contains(query, `address=~"52:54:00:11:22:33"`)) {
					t.Errorf("must resolve the exact guest MAC: %s", query)
				}
				if !scenario.macOnly && !strings.Contains(query, `nodename="`+scenario.domain+`"`) {
					t.Errorf("must resolve the exact hostname: %s", query)
				}
				result := []interface{}{}
				for i := 0; i < scenario.count; i++ {
					stamp := now.Unix()
					if scenario.stale {
						stamp -= 300
					}
					instance := "10.10.31.10:3003"
					job := "ccvm"
					if scenario.macOnly {
						instance = "10.10.1.12:3003"
						job = "scvm"
					}
					if scenario.want != "" {
						instance = scenario.want
					}
					if scenario.job != "" {
						job = scenario.job
					}
					if i > 0 {
						instance = "10.10.1.13:3003"
					}
					result = append(result, map[string]interface{}{"metric": map[string]string{"instance": instance, "job": job}, "values": [][]interface{}{{stamp, "1"}}})
				}
				json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "data": map[string]interface{}{"resultType": "matrix", "result": result}})
			}))
			defer prom.Close()
			got, err := resolveWallSystemVMExporter(context.Background(), prom.Client(), prom.URL, scenario.domain, scenario.mac, now)
			if scenario.count > 1 || scenario.failure {
				if err == nil || got != nil {
					t.Fatal("an ambiguous or failed lookup must not choose an exporter")
				}
				return
			}
			if scenario.want != "" {
				if err != nil || got == nil || got.instance != scenario.want {
					t.Fatalf("unexpected match: %#v, %v", got, err)
				}
			} else if got != nil || err != nil {
				t.Fatalf("missing data must stay missing: %#v, %v", got, err)
			}
			if strings.HasPrefix(scenario.domain, "i-") || strings.HasPrefix(scenario.domain, "r-") {
				if queries != 0 {
					t.Fatal("Mold VM domains must retain their existing libvirt path")
				}
			}
		})
	}
}

func TestWallMoldSystemVMExporterAmbiguousJobs(t *testing.T) {
	now := time.Now()
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		if !strings.Contains(query, `node_uname_info{nodename="s-173-VM"}`) {
			t.Errorf("Mold jobs must be resolved by exact guest identity: %s", query)
		}
		result := []interface{}{}
		for _, job := range []string{"system-vms", "storage-vms"} {
			result = append(result, map[string]interface{}{"metric": map[string]string{"instance": "10.10.31.8:3003", "job": job}, "values": [][]interface{}{{now.Unix(), "1"}}})
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "data": map[string]interface{}{"resultType": "matrix", "result": result}})
	}))
	defer prom.Close()
	got, err := resolveWallSystemVMExporter(context.Background(), prom.Client(), prom.URL, "s-173-VM", "", now)
	if got != nil || err == nil {
		t.Fatal("duplicate instance across jobs must not choose one arbitrarily")
	}
}

func TestWallMoldSystemVMTrendExactGuest(t *testing.T) {
	now := time.Now()
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		if !strings.Contains(query, `instance="10.10.31.8:3003"`) || !strings.Contains(query, `job="mold-system-vms"`) || strings.Contains(query, "libvirt_") {
			t.Errorf("Mold guest trends must retain resolved node exporter identity: %s", query)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "data": map[string]interface{}{"resultType": "matrix", "result": []interface{}{map[string]interface{}{
			"metric": map[string]string{"instance": "10.10.31.8:3003", "job": "mold-system-vms", "mountpoint": "/"}, "values": [][]interface{}{{now.Unix(), "0"}},
		}}}})
	}))
	defer prom.Close()
	response := wallSystemVMTrend(context.Background(), prom.Client(), prom.URL, "s-173-VM", wallGuestExporter{instance: "10.10.31.8:3003", job: "mold-system-vms"}, now.Add(-time.Hour), now, time.Minute)
	if len(response.Warnings) > 0 || response.RawLabels["source"] != "node_exporter" {
		t.Fatalf("unexpected guest response: %#v", response)
	}
	for _, metric := range response.Series {
		if metric.LastValue == nil || *metric.LastValue != 0 {
			t.Fatal("measured zero must be preserved")
		}
		if (metric.Key == "networkRx" || metric.Key == "networkTx") && metric.Unit != "bps" {
			t.Fatal("node exporter network rates must retain API bit/s units")
		}
	}
}

func TestWallSystemVMTrendExactInstanceAndZero(t *testing.T) {
	now := time.Now()
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		if !strings.Contains(query, `instance="10.10.31.10:3003"`) || !strings.Contains(query, `job="ccvm"`) || strings.Contains(query, "libvirt_") {
			t.Errorf("guest measurements must retain the exact exporter: %s", query)
		}
		value := "0"
		if strings.Contains(query, "node_filesystem_size_bytes") && !strings.Contains(query, " - ") {
			value = "21474836480"
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "data": map[string]interface{}{"resultType": "matrix", "result": []interface{}{map[string]interface{}{
			"metric": map[string]string{"instance": "10.10.31.10:3003", "job": "ccvm", "mountpoint": "/"}, "values": [][]interface{}{{now.Unix(), value}},
		}}}})
	}))
	defer prom.Close()
	response := wallSystemVMTrend(context.Background(), prom.Client(), prom.URL, "ccvm", wallGuestExporter{instance: "10.10.31.10:3003", job: "ccvm"}, now.Add(-time.Hour), now, time.Minute)
	if len(response.Series) != 9 || len(response.Warnings) > 0 || response.RawLabels["source"] != "node_exporter" {
		t.Fatalf("unexpected guest response: %#v", response)
	}
	for _, metric := range response.Series {
		if metric.LastValue == nil {
			t.Fatal("a measured zero must remain present")
		}
	}
	if *response.Series[0].LastValue != 0 || *response.Series[1].LastValue != 0 || *response.Series[6].LastValue != 21474836480 {
		t.Fatal("CPU, memory and root filesystem must retain their actual units and zero")
	}
}

func TestWallSystemVMLiveMetrics(t *testing.T) {
	promURL := os.Getenv("NETDIVE_WALL_TEST_URL")
	if promURL == "" {
		t.Skip("explicit live Wall endpoint required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 10 * time.Second}
	now := time.Now()
	exporter, err := resolveWallSystemVMExporter(ctx, client, promURL, "ccvm", "", now)
	if err != nil || exporter == nil {
		t.Fatalf("live CCVM identity lookup failed: %v", err)
	}
	response := wallSystemVMTrend(ctx, client, promURL, "ccvm", *exporter, now.Add(-time.Hour), now, time.Minute)
	for _, key := range []string{"cpu", "memory", "rootDiskTotal"} {
		found := false
		for _, metric := range response.Series {
			if metric.Key == key && metric.LastValue != nil {
				found = true
				t.Logf("%s: %.2f (%s)", key, *metric.LastValue, exporter.instance)
			}
		}
		if !found {
			t.Fatalf("live %s was not returned", key)
		}
	}
}
