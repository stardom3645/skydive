package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestKubernetesProbeDefaultsAndOverrides(t *testing.T) {
	const key = "analyzer.topology.k8s.probes"
	const env = "SKYDIVE_ANALYZER_TOPOLOGY_K8S_PROBES"
	t.Setenv(env, "")
	want := []string{"cluster", "namespace", "node", "pod", "service", "deployment", "replicaset", "daemonset", "statefulset", "ingress", "networkpolicy"}
	if got := GetStringSlice(key); !reflect.DeepEqual(got, want) {
		t.Fatalf("default resources: got %v, want %v", got, want)
	}
	v := viper.New()
	v.SetDefault(key, want)
	v.SetEnvPrefix("SKYDIVE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()
	v.SetTypeByDefaultValue(true)
	file := filepath.Join(t.TempDir(), "analyzer.yml")
	if err := os.WriteFile(file, []byte("analyzer:\n  topology:\n    k8s:\n      probes: [deployment, pod]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := LoadConfig(v, "file", []string{file}); err != nil {
		t.Fatal(err)
	}
	if got := v.GetStringSlice(key); !reflect.DeepEqual(got, []string{"deployment", "pod"}) {
		t.Fatalf("explicit file must still override defaults: %v", got)
	}
	t.Setenv(env, "deployment replicaset pod")
	if got := v.GetStringSlice(key); !reflect.DeepEqual(got, []string{"deployment", "replicaset", "pod"}) {
		t.Fatalf("environment must still override file: %v", got)
	}
}
