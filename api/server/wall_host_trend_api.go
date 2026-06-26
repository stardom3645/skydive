package server

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	shttp "github.com/skydive-project/skydive/graffiti/http"
)

const (
	defaultWallPrometheusURL = "http://127.0.0.1:3001"
	defaultTrendRange        = 3 * time.Hour
	defaultTrendStep         = 60 * time.Second
)

type wallHostTrendResponse struct {
	Host      string                 `json:"host"`
	Range     string                 `json:"range"`
	Step      string                 `json:"step"`
	Start     int64                  `json:"start"`
	End       int64                  `json:"end"`
	PromURL   string                 `json:"promUrl,omitempty"`
	Series    []wallHostTrendSeries  `json:"series"`
	Warnings  []string               `json:"warnings,omitempty"`
	RawLabels map[string]interface{} `json:"rawLabels,omitempty"`
}

type wallHostTrendSeries struct {
	Key       string                 `json:"key"`
	Label     string                 `json:"label"`
	Unit      string                 `json:"unit"`
	Query     string                 `json:"query,omitempty"`
	Values    []wallHostTrendPoint   `json:"values"`
	LastValue *float64               `json:"lastValue,omitempty"`
	Labels    map[string]interface{} `json:"labels,omitempty"`
}

type wallHostTrendPoint struct {
	Timestamp int64    `json:"timestamp"`
	Value     *float64 `json:"value"`
}

type prometheusQueryRangeResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]interface{} `json:"metric"`
			Values [][]interface{}        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func RegisterWallHostTrendAPI(httpServer *shttp.Server) {
	httpServer.Router.HandleFunc("/api/wall/hosts/trend", handleWallHostTrend).Methods("GET")
}

func handleWallHostTrend(w http.ResponseWriter, r *http.Request) {
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	managementIP := strings.TrimSpace(r.URL.Query().Get("managementIp"))
	ip := strings.TrimSpace(r.URL.Query().Get("ip"))

	if len(uniqueNonEmptyStrings(host, name, managementIP, ip)) == 0 {
		writeWallHostTrendError(w, http.StatusBadRequest, "host, name, managementIp or ip is required.")
		return
	}

	trendRange := parseDurationOrDefault(r.URL.Query().Get("range"), defaultTrendRange)
	step := parseDurationOrDefault(r.URL.Query().Get("step"), defaultTrendStep)
	if step < 30*time.Second {
		step = 30 * time.Second
	}

	end := time.Now()
	start := end.Add(-trendRange)
	prometheusJob := firstNonEmptyString(strings.TrimSpace(r.URL.Query().Get("job")), "cube")
	prometheusPort := firstNonEmptyString(strings.TrimSpace(r.URL.Query().Get("port")), "3003")
	prometheusInstances := prometheusInstanceCandidates(prometheusPort, managementIP, ip, host, name)

	prometheusURL := wallPrometheusURL()
	client := &http.Client{Timeout: 10 * time.Second}
	queryTemplates := wallHostTrendQueryTemplates()
	series := make([]wallHostTrendSeries, 0, len(queryTemplates))
	warnings := make([]string, 0)

	for _, item := range queryTemplates {
		var lastErr error

		for _, instance := range prometheusInstances {
			item.Query = wallHostTrendQuery(item.Key, instance, prometheusJob)
			result, err := queryPrometheusRange(client, prometheusURL, item.Query, start, end, step)
			if err != nil {
				lastErr = err
				continue
			}

			points, labels := pickPrometheusSeries(result)
			if len(points) == 0 {
				continue
			}

			item.Values = points
			item.Labels = labels
			item.LastValue = lastTrendValue(points)
			break
		}

		if item.Values == nil {
			item.Values = []wallHostTrendPoint{}
			if lastErr != nil {
				warnings = append(warnings, fmt.Sprintf("%s query failed: %s", item.Key, lastErr.Error()))
			} else {
				warnings = append(warnings, fmt.Sprintf("%s data not found for instances: %s", item.Key, strings.Join(prometheusInstances, ", ")))
			}
		}

		series = append(series, item)
	}

	writeWallHostTrendJSON(w, http.StatusOK, wallHostTrendResponse{
		Host:     firstNonEmptyString(host, name, managementIP, ip),
		Range:    trendRange.String(),
		Step:     step.String(),
		Start:    start.Unix(),
		End:      end.Unix(),
		PromURL:  prometheusURL,
		Series:   series,
		Warnings: warnings,
	})
}

func wallHostTrendQueryTemplates() []wallHostTrendSeries {
	return []wallHostTrendSeries{
		{Key: "cpu", Label: "CPU Usage", Unit: "percent"},
		{Key: "memory", Label: "Memory Usage", Unit: "percent"},
		{Key: "storageIops", Label: "Storage IOPS", Unit: "iops"},
		{Key: "networkRx", Label: "RX", Unit: "bps"},
		{Key: "networkTx", Label: "TX", Unit: "bps"},
		{Key: "networkDrops", Label: "Network Drops", Unit: "count"},
	}
}

func wallHostTrendQuery(key, prometheusInstance, prometheusJob string) string {
	diskDeviceFilter := `device!~"loop.*|ram.*|fd.*|sr.*|dm-.*|zram.*"`
	networkDeviceFilter := `device!~"lo|veth.*|docker.*|br.*|virbr.*|tap.*"`

	switch key {
	case "cpu":
		return fmt.Sprintf(
			`sum (sum by (mode) (irate(node_cpu_seconds_total{instance="%s", job="%s", mode=~"(irq|nice|softirq|steal|system|user|iowait)"}[1m])) / scalar(sum(irate(node_cpu_seconds_total{instance="%s", job="%s"}[1m]))) * 100)`,
			prometheusInstance,
			prometheusJob,
			prometheusInstance,
			prometheusJob,
		)
	case "memory":
		return fmt.Sprintf(
			`(1 - (node_memory_MemAvailable_bytes{instance="%s", job="%s"} / node_memory_MemTotal_bytes{instance="%s", job="%s"})) * 100`,
			prometheusInstance,
			prometheusJob,
			prometheusInstance,
			prometheusJob,
		)
	case "storageIops":
		return fmt.Sprintf(
			`sum(rate(node_disk_reads_completed_total{instance="%s", job="%s", %s}[1m]) + rate(node_disk_writes_completed_total{instance="%s", job="%s", %s}[1m]))`,
			prometheusInstance,
			prometheusJob,
			diskDeviceFilter,
			prometheusInstance,
			prometheusJob,
			diskDeviceFilter,
		)
	case "networkRx":
		return fmt.Sprintf(
			`sum(rate(node_network_receive_bytes_total{instance="%s", job="%s", %s}[1m])) * 8`,
			prometheusInstance,
			prometheusJob,
			networkDeviceFilter,
		)
	case "networkTx":
		return fmt.Sprintf(
			`sum(rate(node_network_transmit_bytes_total{instance="%s", job="%s", %s}[1m])) * 8`,
			prometheusInstance,
			prometheusJob,
			networkDeviceFilter,
		)
	case "networkDrops":
		return fmt.Sprintf(
			`sum(increase(node_network_receive_drop_total{instance="%s", job="%s", %s}[1m]) + increase(node_network_transmit_drop_total{instance="%s", job="%s", %s}[1m]) + increase(node_network_receive_errs_total{instance="%s", job="%s", %s}[1m]) + increase(node_network_transmit_errs_total{instance="%s", job="%s", %s}[1m]))`,
			prometheusInstance,
			prometheusJob,
			networkDeviceFilter,
			prometheusInstance,
			prometheusJob,
			networkDeviceFilter,
			prometheusInstance,
			prometheusJob,
			networkDeviceFilter,
			prometheusInstance,
			prometheusJob,
			networkDeviceFilter,
		)
	default:
		return ""
	}
}

func queryPrometheusRange(client *http.Client, prometheusURL, query string, start, end time.Time, step time.Duration) (*prometheusQueryRangeResponse, error) {
	baseURL, err := url.Parse(strings.TrimRight(prometheusURL, "/") + "/api/v1/query_range")
	if err != nil {
		return nil, err
	}

	params := baseURL.Query()
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(start.Unix(), 10))
	params.Set("end", strconv.FormatInt(end.Unix(), 10))
	params.Set("step", strconv.Itoa(int(step.Seconds())))
	baseURL.RawQuery = params.Encode()

	req, err := http.NewRequest(http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return nil, err
	}

	if token := strings.TrimSpace(os.Getenv("WALL_PROMETHEUS_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("prometheus returned status %d", resp.StatusCode)
	}

	var parsed prometheusQueryRangeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	if parsed.Status != "success" {
		if parsed.Error != "" {
			return nil, fmt.Errorf(parsed.Error)
		}
		return nil, fmt.Errorf("prometheus query was not successful")
	}

	return &parsed, nil
}

func pickPrometheusSeries(result *prometheusQueryRangeResponse) ([]wallHostTrendPoint, map[string]interface{}) {
	if result == nil || len(result.Data.Result) == 0 {
		return []wallHostTrendPoint{}, nil
	}

	bestIndex := 0
	bestLength := 0
	for i, item := range result.Data.Result {
		if len(item.Values) > bestLength {
			bestIndex = i
			bestLength = len(item.Values)
		}
	}

	item := result.Data.Result[bestIndex]
	points := make([]wallHostTrendPoint, 0, len(item.Values))
	for _, raw := range item.Values {
		if len(raw) < 2 {
			continue
		}

		timestamp := parsePrometheusTimestamp(raw[0])
		value := parsePrometheusFloat(raw[1])
		points = append(points, wallHostTrendPoint{
			Timestamp: timestamp,
			Value:     value,
		})
	}

	return points, item.Metric
}

func parsePrometheusTimestamp(value interface{}) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case json.Number:
		i, _ := v.Int64()
		return i
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return int64(f)
	default:
		return 0
	}
}

func parsePrometheusFloat(value interface{}) *float64 {
	var parsed float64
	var err error

	switch v := value.(type) {
	case string:
		parsed, err = strconv.ParseFloat(v, 64)
	case float64:
		parsed = v
	case json.Number:
		parsed, err = v.Float64()
	default:
		return nil
	}

	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil
	}

	return &parsed
}

func lastTrendValue(points []wallHostTrendPoint) *float64 {
	for i := len(points) - 1; i >= 0; i-- {
		if points[i].Value != nil {
			return points[i].Value
		}
	}
	return nil
}

func wallPrometheusURL() string {
	for _, key := range []string{"NETDIVE_WALL_PROMETHEUS_URL", "WALL_PROMETHEUS_URL", "PROMETHEUS_URL"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return strings.TrimRight(value, "/")
		}
	}
	return defaultWallPrometheusURL
}

func prometheusInstanceCandidates(port string, values ...string) []string {
	hosts := make([]string, 0, len(values)*3)
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			host := normalizePrometheusHostCandidate(item)
			if host == "" {
				continue
			}
			hosts = append(hosts, host)
			lower := strings.ToLower(host)
			if lower != host {
				hosts = append(hosts, lower)
			}
			if short := strings.Split(host, ".")[0]; short != host {
				hosts = append(hosts, short)
				hosts = append(hosts, strings.ToLower(short))
			}
		}
	}

	hosts = uniqueNonEmptyStrings(hosts...)
	instances := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if strings.Contains(host, ":") {
			instances = append(instances, host)
			continue
		}
		instances = append(instances, host+":"+port)
	}

	return uniqueNonEmptyStrings(instances...)
}

func normalizePrometheusHostCandidate(value string) string {
	host := strings.TrimSpace(value)
	if host == "" {
		return ""
	}
	if slash := strings.Index(host, "/"); slash >= 0 {
		host = strings.TrimSpace(host[:slash])
	}
	host = strings.Trim(host, "[]")
	return host
}

func parseDurationOrDefault(value string, fallback time.Duration) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}

	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}

	return duration
}

func uniqueNonEmptyStrings(values ...string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))

	for _, value := range values {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}

	return result
}

func writeWallHostTrendJSON(w http.ResponseWriter, status int, response wallHostTrendResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func writeWallHostTrendError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"message": message,
	})
}
