package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/skydive-project/skydive/common"
	"github.com/skydive-project/skydive/config"
)

func TestMoldRelatedServicesUsesExistingPortalSettings(t *testing.T) {
	settings := map[string]string{
		"endpoint.url":                    "http://mold.example:8080/client/api",
		"monitoring.wall.portal.protocol": "https", "monitoring.wall.portal.domain": "wall.example",
		"monitoring.wall.portal.port": "8081", "cube.portal.port": "19100",
	}
	got := buildMoldRelatedServices(settings, "")
	if got.Mold.URL != "http://mold.example:8080/client/" || got.Wall.URL != "https://wall.example:8081" || got.Cube.Port != 19100 {
		t.Fatalf("unexpected portal links: %#v", got)
	}
	settings["monitoring.wall.portal.domain"] = ""
	settings["endpoint.url"] = "http://localhost:8080/client/api"
	got = buildMoldRelatedServices(settings, "http://10.10.31.10:8080/client/#/accountuser?username=admin")
	if got.Mold.URL != "http://10.10.31.10:8080/client/" || got.Wall.URL != "https://10.10.31.10:8081" {
		t.Fatalf("existing externally reachable host resolver was not used: %#v", got)
	}
}

func TestMoldRelatedServicesDisablesMissingAndInvalidSettings(t *testing.T) {
	for _, settings := range []map[string]string{
		{},
		{"endpoint.url": "javascript:alert(1)", "monitoring.wall.portal.protocol": "file", "monitoring.wall.portal.domain": "wall.example", "monitoring.wall.portal.port": "8081", "cube.portal.port": "65536"},
		{"endpoint.url": "https://user:password@mold.example", "monitoring.wall.portal.protocol": "https", "monitoring.wall.portal.domain": "wall.example/path", "monitoring.wall.portal.port": "8081", "cube.portal.port": "0"},
	} {
		got := buildMoldRelatedServices(settings, "http://fallback.example/client/")
		if got.Mold.URL != "" || got.Wall.URL != "" || got.Cube.Port != 0 || got.Mold.Message == "" || got.Wall.Message == "" || got.Cube.Message == "" {
			t.Fatalf("invalid configuration must be unavailable: %#v", got)
		}
	}
}

func useMoldPortalTestServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	previous := config.GetString("mold.api.endpoint")
	config.Set("mold.api.endpoint", server.URL)
	common.SetMoldAPICredentialsStore(kubernetesListTestCredentials{})
	t.Cleanup(func() {
		server.Close()
		config.Set("mold.api.endpoint", previous)
		common.SetMoldAPICredentialsStore(nil)
	})
}

func TestMoldCubeHostsPaginatesAndUsesManagementIP(t *testing.T) {
	pages := 0
	useMoldPortalTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("command") != "listHosts" || query.Get("type") != "Routing" || query.Get("pagesize") != "500" || query.Get("signature") == "" {
			t.Errorf("unexpected host lookup: %v", query)
		}
		pages++
		items := []map[string]interface{}{}
		if query.Get("page") == "1" {
			for i := 0; i < 500; i++ {
				items = append(items, map[string]interface{}{"id": fmt.Sprintf("host-%03d", i), "name": fmt.Sprintf("host-%03d", i), "type": "Routing", "ipaddress": "10.10.31.1", "publicipaddress": "192.0.2.1"})
			}
		} else if query.Get("page") == "2" {
			items = append(items, map[string]interface{}{"id": "missing-ip", "name": "missing-ip", "type": "Routing", "publicipaddress": "192.0.2.2"})
		} else {
			t.Errorf("unexpected extra page")
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"listhostsresponse": map[string]interface{}{"count": 501, "host": items}})
	})
	hosts, err := listMoldCubeHosts(context.Background())
	if err != nil || len(hosts) != 501 || pages != 2 {
		t.Fatalf("host pagination failed: hosts=%d pages=%d err=%v", len(hosts), pages, err)
	}
	if hosts[0].ManagementIP != "10.10.31.1" || hosts[500].ManagementIP != "" {
		t.Fatalf("must not replace management IP with public IP")
	}
}

func TestMoldCubeHostsReportsHTTP200ErrorEnvelope(t *testing.T) {
	useMoldPortalTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errorresponse":{"errorcode":401,"errortext":"denied"}}`))
	})
	if _, err := listMoldCubeHosts(context.Background()); err == nil {
		t.Fatal("Mold error envelope was treated as an empty successful host list")
	}
}
