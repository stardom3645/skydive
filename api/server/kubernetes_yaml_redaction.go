package server

import (
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Pod schemas separate references/configuration from actual credential values.
// Never redact a subtree merely because its field name contains token/key/id.
func redactKubernetesYAML(value interface{}) bool {
	return redactKubernetesYAMLAt(value, nil)
}

func redactKubernetesYAMLAt(value interface{}, path []string) bool {
	location := strings.Join(path, ".")
	if kubernetesYAMLReferencePath(path) {
		return false // No reference is resolved and no Secret API is called.
	}
	changed := false
	switch object := value.(type) {
	case map[string]interface{}:
		for key, child := range object {
			next := append(append([]string{}, path...), key)
			mask := false
			switch {
			case location == "metadata.annotations":
				text, ok := child.(string)
				mask = !ok || !kubernetesYAMLOperationalAnnotation(key, text)
			case location == "metadata.labels":
				mask = kubernetesYAMLCredentialName(key)
			case key == "value" && kubernetesYAMLContainerPath(path, ".env.*"):
				name, _ := object["name"].(string)
				text, _ := child.(string)
				mask = !kubernetesYAMLSafeEnvironment(name, text)
			case key == "value" && strings.HasSuffix(location, ".httpHeaders.*"):
				mask = true // Header names stay visible; custom values can authenticate.
			case (key == "command" || key == "args") && (kubernetesYAMLContainerPath(path, "") || strings.HasSuffix(location, ".exec")):
				// Arbitrary arguments/scripts cannot reliably be distinguished from
				// credentials. Keep only an unambiguous executable path.
				if values, ok := child.([]interface{}); ok {
					for i, item := range values {
						text, _ := item.(string)
						if key == "command" && i == 0 && kubernetesYAMLExecutable.MatchString(text) {
							continue
						}
						replacement, masked := maskKubernetesYAMLValue(item)
						if masked {
							values[i], changed = replacement, true
						}
					}
				}
				continue
			case location == "spec.volumes.*.csi.volumeAttributes" || location == "spec.volumes.*.flexVolume.options":
				mask = true // Driver-defined values have no portable secret schema.
			}
			if text, ok := child.(string); ok && kubernetesYAMLContainsCredential(text) && !kubernetesYAMLReferencePath(next) {
				mask = true
			}
			if mask {
				replacement, masked := maskKubernetesYAMLValue(child)
				if masked {
					object[key], changed = replacement, true
				}
			} else if redactKubernetesYAMLAt(child, next) {
				changed = true
			}
		}
	case []interface{}:
		for _, child := range object {
			if redactKubernetesYAMLAt(child, append(append([]string{}, path...), "*")) {
				changed = true
			}
		}
	}
	return changed
}

func maskKubernetesYAMLValue(value interface{}) (interface{}, bool) {
	changed := false
	switch object := value.(type) {
	case map[string]interface{}:
		for key, child := range object {
			replacement, masked := maskKubernetesYAMLValue(child)
			object[key] = replacement
			changed = changed || masked
		}
		return object, changed
	case []interface{}:
		for i, child := range object {
			replacement, masked := maskKubernetesYAMLValue(child)
			object[i] = replacement
			changed = changed || masked
		}
		return object, changed
	case string:
		if object == "" || object == kubernetesYAMLRedacted {
			return value, false
		}
	case nil:
		return value, false
	}
	return kubernetesYAMLRedacted, true
}

func kubernetesYAMLContainerPath(path []string, suffix string) bool {
	location := strings.Join(path, ".")
	for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
		if location == "spec."+field+".*"+suffix {
			return true
		}
	}
	return false
}

func kubernetesYAMLReferencePath(path []string) bool {
	if len(path) < 2 || path[0] != "spec" {
		return false
	}
	for _, part := range path[1:] {
		switch part {
		case "secretKeyRef", "configMapKeyRef", "secretRef", "configMapRef", "imagePullSecrets", "secret", "configMap", "nodePublishSecretRef", "serviceAccountToken", "secretName", "secretFile":
			return true
		}
	}
	return false
}

var kubernetesYAMLCamelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
var kubernetesYAMLNameSeparators = regexp.MustCompile(`[^a-z0-9]+`)

// This applies to user-defined label names and URL parameters, not Pod schema
// field names. token-expiration, token-id, keyboard and keycloak are not secrets.
func kubernetesYAMLCredentialName(name string) bool {
	name = name[strings.LastIndex(name, "/")+1:]
	parts := kubernetesYAMLNameSeparators.Split(strings.ToLower(kubernetesYAMLCamelBoundary.ReplaceAllString(name, "${1}_${2}")), -1)
	joined := strings.Join(parts, "")
	switch joined {
	case "password", "passwd", "pwd", "token", "secret", "apikey", "apitoken", "accesstoken", "refreshtoken", "idtoken", "authtoken", "bearertoken", "auth", "authorization", "credential", "credentials", "clientsecret", "secretkey", "privatekey", "clientkey", "accesskey", "awsaccesskeyid", "awssecretaccesskey", "signingkey", "sshprivatekey":
		return true
	}
	if len(parts) > 1 {
		switch parts[len(parts)-1] {
		case "password", "passwd", "credential", "credentials", "token", "secret", "apikey", "privatekey", "secretkey", "authorization":
			return true
		}
		switch strings.Join(parts[len(parts)-2:], "") {
		case "apikey", "privatekey", "clientkey", "secretkey", "accesskey", "signingkey":
			return true
		}
	}
	return false
}

var kubernetesYAMLCredentialValue = regexp.MustCompile(`(?i)(-----BEGIN (?:RSA |EC |OPENSSH |DSA |ENCRYPTED )?PRIVATE KEY-----|\b(?:bearer|basic)\s+[a-z0-9+/_.=-]+|\beyJ[a-z0-9_-]+\.[a-z0-9_-]+\.[a-z0-9_-]+|\b(?:password|passwd|api[_-]?key|(?:access[_-]?|refresh[_-]?)?token|secret|client[_-]?secret|private[_-]?key|client[_-]?key|authorization)\s*[=:]\s*\S+)`)
var kubernetesYAMLURL = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s<>"']+`)
var kubernetesYAMLExecutable = regexp.MustCompile(`^/(?:[a-zA-Z0-9_.+-]+/)*[a-zA-Z0-9_.+-]+$`)
var kubernetesYAMLContainerID = regexp.MustCompile(`^(?:(?:containerd|docker|cri-o)://)?[a-fA-F0-9]{12,128}$`)

func kubernetesYAMLContainsCredential(value string) bool {
	if kubernetesYAMLCredentialValue.MatchString(value) {
		return true
	}
	for _, text := range kubernetesYAMLURL.FindAllString(value, -1) {
		parsed, err := url.Parse(text)
		if err != nil {
			return true
		}
		if parsed.User != nil {
			return true
		}
		for name := range parsed.Query() {
			lower := strings.ToLower(name)
			if kubernetesYAMLCredentialName(name) || lower == "key" || lower == "sig" || lower == "signature" || lower == "x-amz-signature" || lower == "x-amz-credential" || lower == "x-amz-security-token" || lower == "x-goog-signature" || lower == "x-goog-credential" {
				return true
			}
		}
	}
	return false
}

// Unknown annotations may contain serialized manifests, scripts or opaque
// credentials. Only recognized diagnostic fields with validated values pass.
func kubernetesYAMLOperationalAnnotation(name, value string) bool {
	if kubernetesYAMLContainsCredential(value) {
		return false
	}
	switch name {
	case "cni.projectcalico.org/containerID":
		return kubernetesYAMLContainerID.MatchString(value)
	case "cni.projectcalico.org/podIP", "cni.projectcalico.org/podIPs":
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if net.ParseIP(part) == nil {
				if _, _, err := net.ParseCIDR(part); err != nil {
					return false
				}
			}
		}
		return value != ""
	case "kubectl.kubernetes.io/restartedAt":
		_, err := time.Parse(time.RFC3339, value)
		return err == nil
	case "prometheus.io/scrape":
		return value == "true" || value == "false"
	case "prometheus.io/port":
		port, err := strconv.Atoi(value)
		return err == nil && port > 0 && port <= 65535
	case "prometheus.io/scheme":
		return value == "http" || value == "https"
	case "prometheus.io/path":
		return kubernetesYAMLExecutable.MatchString(value)
	}
	return false
}

func kubernetesYAMLSafeEnvironment(name, value string) bool {
	switch name {
	case "LOG_LEVEL":
		switch strings.ToLower(value) {
		case "trace", "debug", "info", "warn", "warning", "error", "fatal", "off":
			return true
		}
	case "NODE_ENV":
		return value == "production" || value == "development" || value == "test"
	case "TZ":
		_, err := time.LoadLocation(value)
		return err == nil
	case "PORT":
		port, err := strconv.Atoi(value)
		return err == nil && port > 0 && port <= 65535
	}
	return false
}
