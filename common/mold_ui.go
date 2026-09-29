package common

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

const moldServerPropertiesPath = "/etc/cloudstack/management/server.properties"

func parseMoldServerProperties(content string) map[string]string {
	properties := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		properties[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return properties
}

func moldUIURLFromProperties(content, apiEndpoint string) (string, error) {
	endpoint, err := url.Parse(strings.TrimSpace(apiEndpoint))
	if err != nil || endpoint.Hostname() == "" {
		return "", fmt.Errorf("invalid Mold API endpoint")
	}

	properties := parseMoldServerProperties(content)
	scheme := ""
	port := ""
	if strings.EqualFold(properties["https.enable"], "true") {
		scheme = "https"
		port = properties["https.port"]
		if port == "" {
			port = "8443"
		}
	} else if strings.EqualFold(properties["http.enable"], "true") {
		scheme = "http"
		port = properties["http.port"]
		if port == "" {
			port = "8080"
		}
	} else {
		return "", fmt.Errorf("Mold HTTP and HTTPS listeners are disabled")
	}

	host := endpoint.Hostname()
	if (scheme == "https" && port != "443") || (scheme == "http" && port != "80") {
		host = net.JoinHostPort(host, port)
	}
	return fmt.Sprintf("%s://%s/client/#/accountuser?username=admin", scheme, host), nil
}

func moldUIURLFromAPIEndpoint(apiEndpoint string) (string, error) {
	endpoint, err := url.Parse(strings.TrimSpace(apiEndpoint))
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return "", fmt.Errorf("invalid Mold API endpoint")
	}
	return fmt.Sprintf("%s://%s/client/#/accountuser?username=admin", endpoint.Scheme, endpoint.Host), nil
}

// GetMoldAccountUserURL returns a non-secret browser URL for the Mold admin
// account-user screen. The management server's listener configuration is the
// source of truth; the configured API endpoint is only a safe fallback.
func GetMoldAccountUserURL() string {
	apiEndpoint := GetMoldAPIConfig().Endpoint
	if content, err := os.ReadFile(moldServerPropertiesPath); err == nil {
		if uiURL, err := moldUIURLFromProperties(string(content), apiEndpoint); err == nil {
			return uiURL
		}
	}
	uiURL, _ := moldUIURLFromAPIEndpoint(apiEndpoint)
	return uiURL
}
