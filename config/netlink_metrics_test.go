package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestNetlinkMetricsUpdateDefaultAndOverrides(t *testing.T) {
	const key = "agent.topology.netlink.metrics_update"
	const env = "SKYDIVE_AGENT_TOPOLOGY_NETLINK_METRICS_UPDATE"
	t.Setenv(env, "")
	interval := GetInt(key)
	if interval != 10 {
		t.Fatalf("default interface metrics interval: got %d, want 10 seconds", interval)
	}

	// Exercise the production file loader: explicit deployment values must
	// override the new default, including legacy 30-second configurations.
	for _, seconds := range []int{5, 10, 30} {
		t.Run(fmt.Sprintf("file override %ds", seconds), func(t *testing.T) {
			v := viper.New()
			v.SetDefault(key, interval)
			path := filepath.Join(t.TempDir(), "netdive.yml")
			content := fmt.Sprintf("agent:\n  topology:\n    netlink:\n      metrics_update: %d\n", seconds)
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			if err := LoadConfig(v, "file", []string{path}); err != nil {
				t.Fatal(err)
			}
			if got := v.GetInt(key); got != seconds {
				t.Fatalf("configured interval: got %d, want %d", got, seconds)
			}
		})
	}

	t.Run("environment override", func(t *testing.T) {
		t.Setenv(env, "30")
		if got := GetInt(key); got != 30 {
			t.Fatalf("environment interval: got %d, want 30", got)
		}
	})
}
