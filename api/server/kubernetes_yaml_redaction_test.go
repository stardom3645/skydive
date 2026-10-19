package server

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestKubernetesYAMLRedactionPreservesDiagnosticsAndReferences(t *testing.T) {
	// Compare the entire document, not only the fields that must be hidden.
	// References are metadata already readable through pods/get, not Secret data.
	raw := `{
  "metadata": {
    "name":"web", "namespace":"system", "uid":"pod-uid", "resourceVersion":"42",
    "labels":{"app":"web","app-id":"42","keyboard":"standard","keycloak":"enabled","token-expiration":"3600","token-id":"reference-id","apiKey":"label-api-value","dbPassword":"label-password-value"},
    "annotations": {
      "cni.projectcalico.org/containerID":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      "cni.projectcalico.org/podIP":"10.1.2.3/32",
      "cni.projectcalico.org/podIPs":"10.1.2.3/32,fd00::123/128",
      "kubectl.kubernetes.io/restartedAt":"2026-10-19T10:00:00Z",
      "prometheus.io/scrape":"true","prometheus.io/port":"9090","prometheus.io/path":"/metrics","prometheus.io/scheme":"http",
      "kubectl.kubernetes.io/last-applied-configuration":"embedded-manifest-value",
      "example.com/opaque":"unclassified-annotation-value"
    }
  },
  "spec": {
    "serviceAccountName":"worker", "automountServiceAccountToken":false,
    "imagePullSecrets":[{"name":"registry-secret"}],
    "containers":[{
      "name":"web","image":"registry/app:v1@sha256:0123456789abcdef",
      "resources":{"requests":{"cpu":"100m","memory":"64Mi"},"limits":{"cpu":"1","memory":"128Mi"}},
      "ports":[{"containerPort":8080}],
      "command":["/usr/bin/server"],"args":["--password=argument-value","unclassified-argument-value"],
      "env":[
        {"name":"PASSWORD","value":"inline-password-value"},
        {"name":"SETTING","value":"unclassified-environment-value"},
        {"name":"LOG_LEVEL","value":"info"},{"name":"PORT","value":"8080"},{"name":"TZ","value":"UTC"},{"name":"NODE_ENV","value":"production"},
        {"name":"EMPTY","value":""},
        {"name":"API_TOKEN","valueFrom":{"secretKeyRef":{"name":"auth-token","key":"api-key","optional":false}}},
        {"name":"PUBLIC_SETTING","valueFrom":{"configMapKeyRef":{"name":"settings","key":"public-key","optional":true}}},
        {"name":"MY_IP","valueFrom":{"fieldRef":{"fieldPath":"status.podIP"}}}
      ],
      "envFrom":[{"prefix":"AUTH_","secretRef":{"name":"credentials","optional":false}},{"configMapRef":{"name":"settings"}}],
      "livenessProbe":{"httpGet":{"path":"/healthz","port":8080,"httpHeaders":[{"name":"Authorization","value":"Bearer header-value"}]}}
    }],
    "initContainers":[{"name":"init","env":[{"name":"INIT_KEY","value":"init-value"}]}],
    "ephemeralContainers":[{"name":"debug","command":["/bin/sh","-c","script-value"]}],
    "volumes":[
      {"name":"credentials","secret":{"secretName":"auth-secret","optional":false,"defaultMode":384,"items":[{"key":"password","path":"password.txt"}]}},
      {"name":"projected","projected":{"sources":[{"serviceAccountToken":{"path":"token","audience":"https://kubernetes.default.svc","expirationSeconds":3600}},{"secret":{"name":"projected-secret","items":[{"key":"token","path":"token.txt"}]}}]}},
      {"name":"csi","csi":{"driver":"example.csi","nodePublishSecretRef":{"name":"storage-auth"},"volumeAttributes":{"api-key":"driver-secret-value","custom-option":"driver-opaque-value"}}}
    ]
  },
  "status": {
    "podIP":"10.1.2.3","podIPs":[{"ip":"10.1.2.3"},{"ip":"fd00::123"}],"hostIP":"192.168.1.2","hostIPs":[{"ip":"192.168.1.2"}],
    "containerStatuses":[{"name":"web","containerID":"containerd://0123456789abcdef","image":"registry/app:v1","imageID":"registry/app@sha256:0123456789abcdef","ready":true,"restartCount":0}],
    "message":"diagnostic-message password=message-secret-value"
  }
}`
	expected := raw
	for _, hidden := range []string{"label-api-value", "label-password-value", "embedded-manifest-value", "unclassified-annotation-value", "--password=argument-value", "unclassified-argument-value", "inline-password-value", "unclassified-environment-value", "Bearer header-value", "init-value", "-c", "script-value", "driver-secret-value", "driver-opaque-value", "diagnostic-message password=message-secret-value"} {
		expected = strings.ReplaceAll(expected, `"`+hidden+`"`, `"[REDACTED]"`)
	}
	var document, wanted map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(expected), &wanted); err != nil {
		t.Fatal(err)
	}
	if !redactKubernetesYAML(document) || !reflect.DeepEqual(document, wanted) {
		encoded, _ := json.MarshalIndent(document, "", "  ")
		t.Fatalf("diagnostic fields/references changed or sensitive values exposed:\n%s", encoded)
	}
	if redactKubernetesYAML(document) {
		t.Fatal("redaction should be idempotent")
	}
}

func TestKubernetesYAMLRedactionValidatesOperationalValues(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"cni.projectcalico.org/podIP", "opaque-sensitive-value"},
		{"cni.projectcalico.org/podIPs", "10.0.0.1/32,opaque-sensitive-value"},
		{"cni.projectcalico.org/containerID", "opaque-sensitive-value"},
		{"prometheus.io/path", "/metrics?token=opaque-sensitive-value"},
		{"prometheus.io/port", "opaque-sensitive-value"},
		{"prometheus.io/scrape", "opaque-sensitive-value"},
		{"kubectl.kubernetes.io/restartedAt", "opaque-sensitive-value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			annotations := map[string]interface{}{tc.name: tc.value}
			document := map[string]interface{}{"metadata": map[string]interface{}{"annotations": annotations}}
			if !redactKubernetesYAML(document) || annotations[tc.name] != kubernetesYAMLRedacted {
				t.Fatal("invalid annotation value exposed")
			}
		})
	}
}

func TestKubernetesYAMLRedactionLeavesOrdinaryPodUnchanged(t *testing.T) {
	raw := `{"metadata":{"name":"web","uid":"pod-uid","labels":{"app":"web"},"annotations":{"cni.projectcalico.org/podIPs":"10.1.2.3/32,fd00::123/128"}},"spec":{"automountServiceAccountToken":false,"containers":[{"name":"web","image":"app:1","env":[{"name":"API_TOKEN","valueFrom":{"secretKeyRef":{"name":"auth-secret","key":"token","optional":false}}}]}]},"status":{"podIP":"10.1.2.3","hostIP":"192.168.1.2","containerStatuses":[{"containerID":"containerd://0123456789abcdef","restartCount":0,"ready":false}]}}`
	var document map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(document)
	if redactKubernetesYAML(document) {
		t.Fatal("ordinary diagnostic data and references must not be marked redacted")
	}
	after, _ := json.Marshal(document)
	if string(before) != string(after) {
		t.Fatal("unnecessary content/type/value changes")
	}
}

func TestKubernetesYAMLRedactionRecognizesCredentialValuesWithoutBroadKeyMatching(t *testing.T) {
	for _, value := range []string{
		"-----BEGIN PRIVATE KEY-----\nopaque\n-----END PRIVATE KEY-----",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nopaque",
		"Bearer opaque-value", "Basic dXNlcjpwYXNz", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.signature",
		"https://user:password@example.test/health", "https://example.test/health?api_key=opaque-value",
		"https://example.test/health?key=opaque-value", "https://example.test/file?X-Amz-Signature=opaque-value",
		"failure: client_secret=opaque-value", "failure: access_token=opaque-value", "failure: secret=opaque-value", "failure: private_key=opaque-value",
	} {
		if !kubernetesYAMLContainsCredential(value) {
			t.Fatalf("credential not recognized: %q", value)
		}
	}
	for _, name := range []string{"id", "containerID", "podIP", "key", "keyboard", "keycloak", "token-id", "token-expiration", "secretName", "privateKeyFile"} {
		if kubernetesYAMLCredentialName(name) {
			t.Fatalf("reference/configuration name incorrectly treated as credential: %s", name)
		}
	}
	for _, name := range []string{"password", "apiKey", "DB_TOKEN", "dbPassword", "dbSecret", "myAPIKey", "db_api_key", "ssh_private_key", "example.com/api-token", "serviceAccountToken"} {
		if !kubernetesYAMLCredentialName(name) {
			t.Fatalf("credential label name not recognized: %s", name)
		}
	}
	for _, value := range []string{"containerd://0123456789abcdef", "10.1.2.3/32", "fd00::123", "registry/app:v1@sha256:abcdef", "https://example.test/health?port=8080", "token expired", "keycloak is ready"} {
		if kubernetesYAMLContainsCredential(value) {
			t.Fatalf("diagnostic value incorrectly treated as credential: %q", value)
		}
	}
}
