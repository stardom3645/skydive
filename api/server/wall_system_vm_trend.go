package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type wallGuestExporter struct{ instance, job string }

var wallSystemVMName = regexp.MustCompile(`(?i)^(ccvm|scvm)[0-9]*$`)

// Local control/storage VMs use node_exporter, unlike Mold VM domains.
// Match a guest MAC or its exact nodename; never select a whole job or the
// first exporter when several storage VMs share the same domain name.
func resolveWallSystemVMExporter(ctx context.Context, client *http.Client, prometheusURL, domain, macList string, now time.Time) (*wallGuestExporter, error) {
	if !wallSystemVMName.MatchString(domain) {
		return nil, nil
	}
	job := "scvm"
	if strings.HasPrefix(strings.ToLower(domain), "ccvm") {
		job = "ccvm"
	}
	macs := []string{}
	for _, value := range strings.Split(macList, ",") {
		if mac, err := net.ParseMAC(strings.TrimSpace(value)); err == nil {
			macs = append(macs, strings.ToLower(mac.String()))
		}
	}
	queries := []string{}
	if len(macs) > 0 {
		queries = append(queries, fmt.Sprintf(`node_network_info{job=%q,address=~%q}`, job, prometheusRegexAlternation(macs...)))
	}
	queries = append(queries, fmt.Sprintf(`node_uname_info{job=%q,nodename=%q}`, job, domain))
	for _, query := range queries {
		result, err := queryPrometheusRange(ctx, client, prometheusURL, query, now.Add(-2*time.Minute), now, 30*time.Second)
		if err != nil {
			return nil, err
		}
		matches := map[string]wallGuestExporter{}
		for _, series := range result.Data.Result {
			instance, _ := series.Metric["instance"].(string)
			metricJob, _ := series.Metric["job"].(string)
			if instance == "" || metricJob != job {
				continue
			}
			for i := len(series.Values) - 1; i >= 0; i-- {
				point := series.Values[i]
				if len(point) != 2 || parsePrometheusTimestamp(point[0]) < now.Add(-90*time.Second).Unix() {
					continue
				}
				if value := parsePrometheusFloat(point[1]); value != nil && *value > 0 {
					matches[instance] = wallGuestExporter{instance: instance, job: job}
				}
				break
			}
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("multiple %s exporters match %s; an exact guest NIC MAC is required", job, domain)
		}
		for _, match := range matches {
			return &match, nil
		}
	}
	return nil, nil
}

func wallSystemVMTrend(ctx context.Context, client *http.Client, prometheusURL, domain string, exporter wallGuestExporter, start, end time.Time, step time.Duration) wallHostTrendResponse {
	series := wallHostTrendQueryTemplates()
	warnings := []string{}
	rootDisk := startWallRootDiskQueries(ctx, client, prometheusURL, []string{exporter.instance}, "", exporter.job, start, end, step)
	for i := range series {
		item := &series[i]
		item.Query = wallHostTrendQuery(item.Key, exporter.instance, exporter.job)
		result, err := queryPrometheusRange(ctx, client, prometheusURL, item.Query, start, end, step)
		if err != nil {
			item.Values = []wallHostTrendPoint{}
			warnings = append(warnings, fmt.Sprintf("%s query failed: %s", item.Key, err))
			continue
		}
		item.Values, item.Labels = pickPrometheusSeries(result)
		item.LastValue = lastTrendValue(item.Values)
		if item.LastValue == nil {
			warnings = append(warnings, fmt.Sprintf("%s data not found for %s", item.Key, exporter.instance))
		}
	}
	series = append(series, (<-rootDisk)...)
	return wallHostTrendResponse{Host: domain, Range: end.Sub(start).String(), Step: step.String(), Start: start.Unix(), End: end.Unix(), PromURL: prometheusURL,
		Series: series, Warnings: warnings, RawLabels: map[string]interface{}{"instance": exporter.instance, "job": exporter.job, "source": "node_exporter"}}
}
