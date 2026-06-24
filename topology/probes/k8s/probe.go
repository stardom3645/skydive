/*
 * Copyright 2017 IBM Corp.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy ofthe License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specificlanguage governing permissions and
 * limitations under the License.
 *
 */

package k8s

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/skydive-project/skydive/graffiti/graph"
	"github.com/skydive-project/skydive/graffiti/logging"
	"github.com/skydive-project/skydive/probe"

	"k8s.io/apimachinery/pkg/util/runtime"
)

// LinkHandler creates a linker
type LinkHandler func(g *graph.Graph) probe.Handler

// InitLinkers initializes the listed linkers
func InitLinkers(linkerHandlers []LinkHandler, g *graph.Graph) (linkers []probe.Handler) {
	for _, handler := range linkerHandlers {
		if linker := handler(g); linker != nil {
			linkers = append(linkers, linker)
		}
	}
	return
}

var subprobes = make(map[string]map[string]Subprobe)

func resetSubprobes(manager string) {
	subprobes[manager] = make(map[string]Subprobe)
}

func resetAllSubprobes() {
	subprobes = make(map[string]map[string]Subprobe)
}

// PutSubprobe puts a new subprobe in the subprobes map
func PutSubprobe(manager, name string, subprobe Subprobe) {
	subprobes[manager][name] = subprobe
}

// GetSubprobe returns a specific subprobe
func GetSubprobe(manager, name string) Subprobe {
	return subprobes[manager][name]
}

// GetSubprobesMap returns a map of all the subprobes that belong to manager probe
func GetSubprobesMap(manager string) map[string]Subprobe {
	return subprobes[manager]
}

// ListSubprobes returns the list of Subprobe as ListernerHandler
func ListSubprobes(manager string, types ...string) (handlers []graph.ListenerHandler) {
	for _, t := range types {
		if subprobe := GetSubprobe(manager, t); subprobe != nil {
			handlers = append(handlers, subprobe)
		}
	}
	return
}

func int32ValueOrDefault(value *int32, defaultValue int32) int32 {
	if value == nil {
		return defaultValue
	}
	return *value
}

// Probe for tracking k8s events
type Probe struct {
	graph     *graph.Graph
	manager   string
	subprobes map[string]Subprobe
	linkers   []probe.Handler
	verifiers []probe.Handler
}

// Subprobe describes a probe for a specific Kubernetes resource
// It must implement the ListenerHandler interface so that you
// listen for creation/update/removal of a resource
type Subprobe interface {
	Start() error
	Stop()
	graph.ListenerHandler
}

// Linker defines a k8s linker
type Linker struct {
	*graph.ResourceLinker
}

// OnError implements the LinkerEventListener interface
func (l *Linker) OnError(err error) {
	logging.GetLogger().Error(err)
}

// Start k8s probe
func (p *Probe) Start() error {
	for _, linker := range p.linkers {
		if err := linker.Start(); err != nil {
			return err
		}
	}

	for _, subprobe := range p.subprobes {
		if err := subprobe.Start(); err != nil {
			return err
		}
	}

	for _, verifier := range p.verifiers {
		if err := verifier.Start(); err != nil {
			return err
		}
	}

	return nil
}

// Stop k8s probe
func (p *Probe) Stop() {
	for _, linker := range p.linkers {
		linker.Stop()
	}

	for _, subprobe := range p.subprobes {
		subprobe.Stop()
	}

	for _, verifier := range p.verifiers {
		verifier.Stop()
	}
}

// AppendClusterLinkers appends newly created cluster linker per type
func (p *Probe) AppendClusterLinkers(types ...string) {
	clusterProbe, _ := p.subprobes[Cluster].(*clusterCache)
	if clusterLinker := newClusterLinker(p.graph, p.manager, clusterProbe, types...); clusterLinker != nil {
		p.linkers = append(p.linkers, clusterLinker)
	}
}

// AppendNamespaceLinkers appends newly created namespace linker per type
func (p *Probe) AppendNamespaceLinkers(types ...string) {
	if namespaceLinker := newNamespaceLinker(p.graph, p.manager, types...); namespaceLinker != nil {
		p.linkers = append(p.linkers, namespaceLinker)
	}
}

// NewProbe creates the probe for tracking k8s events
func NewProbe(g *graph.Graph, manager string, subprobes map[string]Subprobe, linkers []probe.Handler, verifiers []probe.Handler) *Probe {
	names := []string{}
	for k := range subprobes {
		names = append(names, k)
	}
	logging.GetLogger().Infof("Probe %s subprobes %v", manager, names)
	return &Probe{
		graph:     g,
		manager:   manager,
		subprobes: subprobes,
		linkers:   linkers,
		verifiers: verifiers,
	}
}

// SubprobeHandler the signature of ctor of a subprobe
type SubprobeHandler func(client interface{}, g *graph.Graph) Subprobe

// InitSubprobes initializes only the subprobes which are enabled
func InitSubprobes(enabled []string, subprobeHandlers map[string]SubprobeHandler, client interface{}, g *graph.Graph, manager, clusterName string) {
	if subprobes[manager] == nil {
		subprobes[manager] = make(map[string]Subprobe)
	}

	if len(enabled) == 0 {
		for name := range subprobeHandlers {
			enabled = append(enabled, name)
		}
	}

	for _, name := range enabled {
		if handler := subprobeHandlers[name]; handler != nil {
			subprobe := handler(client, g)
			if resourceCache, ok := subprobe.(*ResourceCache); ok {
				resourceCache.clusterName = clusterName
			}
			if clusterAware, ok := subprobe.(interface{ SetClusterName(string) }); ok {
				clusterAware.SetClusterName(clusterName)
			}

			PutSubprobe(manager, name, subprobe)
		}
	}
}

var k8sReflectorResourceVersionRe = regexp.MustCompile(`resourceVersion=[0-9]+`)
var k8sDialErrorRe = regexp.MustCompile(`dial tcp ([^:]+:[0-9]+): ([^\n]+)$`)

type k8sErrorRateLimiter struct {
	period time.Duration
	lock   sync.Mutex
	last   map[string]time.Time
}

var k8sErrorLimiter = &k8sErrorRateLimiter{
	period: time.Minute,
	last:   make(map[string]time.Time),
}

func normalizeK8sReflectorError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if match := k8sDialErrorRe.FindStringSubmatch(msg); len(match) == 3 {
		return "kubernetes apiserver " + match[1] + ": " + match[2]
	}
	msg = k8sReflectorResourceVersionRe.ReplaceAllString(msg, "resourceVersion=*")
	return strings.TrimSpace(msg)
}

func logOnError(err error) {
	key := normalizeK8sReflectorError(err)
	if key == "" {
		return
	}

	now := time.Now()
	k8sErrorLimiter.lock.Lock()
	last, seen := k8sErrorLimiter.last[key]
	if seen && now.Sub(last) < k8sErrorLimiter.period {
		k8sErrorLimiter.lock.Unlock()
		return
	}
	k8sErrorLimiter.last[key] = now
	k8sErrorLimiter.lock.Unlock()

	logging.GetLogger().Warningf("%s (repeated identical Kubernetes watch errors are suppressed for %s)", key, k8sErrorLimiter.period)
}

func muteInternalErrors() {
	runtime.ErrorHandlers = []func(error){
		logOnError,
	}
}

func init() {
	muteInternalErrors()
}
