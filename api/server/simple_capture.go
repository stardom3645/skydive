package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	auth "github.com/abbot/go-http-auth"
	uuid "github.com/nu7hatch/gouuid"
	etcd "go.etcd.io/etcd/client/v2"

	"github.com/skydive-project/skydive/api/types"
	"github.com/skydive-project/skydive/flow/probes"
	"github.com/skydive-project/skydive/graffiti/api/rest"
	etcdclient "github.com/skydive-project/skydive/graffiti/etcd/client"
	"github.com/skydive-project/skydive/graffiti/graph"
	"github.com/skydive-project/skydive/graffiti/graph/traversal"
	shttp "github.com/skydive-project/skydive/graffiti/http"
	"github.com/skydive-project/skydive/graffiti/logging"
)

const simpleCapturePrefix = "/simple-capture"
const simpleCapturePcapPrefix = "/simple-capture-pcap"

var simpleCaptureDurations = map[int]struct{}{
	30:  {},
	60:  {},
	180: {},
}

type SimpleCaptureRequest struct {
	NodeTID         string `json:"nodeTID"`
	NodeID          string `json:"nodeID"`
	Scope           string `json:"scope"`
	DurationSeconds int    `json:"durationSeconds"`
	FilterPreset    string `json:"filterPreset"`
	BPF             string `json:"bpf"`
	CaptureType     string `json:"captureType"`
	LayerKeyMode    string `json:"layerKeyMode"`
	RawPacketLimit  int    `json:"rawPacketLimit"`
	HeaderSize      int    `json:"headerSize"`
	ExtraTCPMetric  bool   `json:"extraTCPMetric"`
	IPDefrag        bool   `json:"ipDefrag"`
	ReassembleTCP   bool   `json:"reassembleTCP"`
}

type SimpleCaptureTarget struct {
	NodeTID string `json:"nodeTID"`
	NodeID  string `json:"nodeID"`
	Name    string `json:"name"`
	Type    string `json:"type"`
}

type SimpleCapture struct {
	ID        string               `json:"id"`
	CaptureID string               `json:"captureID"`
	Status    string               `json:"status"`
	StartedAt time.Time            `json:"startedAt"`
	ExpiresAt time.Time            `json:"expiresAt"`
	DeletedAt *time.Time           `json:"deletedAt,omitempty"`
	Request   SimpleCaptureRequest `json:"request"`
	Target    SimpleCaptureTarget  `json:"target"`
	Message   string               `json:"message,omitempty"`
}

type SimpleCaptureAPI struct {
	graph         *graph.Graph
	gremlinParser *traversal.GremlinTraversalParser
	captureAPI    *CaptureAPIHandler
	store         *SimpleCaptureStore
	timers        map[string]*time.Timer
	mutex         sync.Mutex
}

type SimpleCaptureStore struct {
	etcdClient *etcdclient.Client
}

func NewSimpleCaptureStore(etcdClient *etcdclient.Client) *SimpleCaptureStore {
	return &SimpleCaptureStore{etcdClient: etcdClient}
}

func (s *SimpleCaptureStore) path(id string) string {
	return fmt.Sprintf("%s/%s", simpleCapturePrefix, id)
}

func (s *SimpleCaptureStore) pcapPath(id string) string {
	return fmt.Sprintf("%s/%s", simpleCapturePcapPrefix, id)
}

func (s *SimpleCaptureStore) Save(capture *SimpleCapture) error {
	data, err := json.Marshal(capture)
	if err != nil {
		return err
	}
	_, err = s.etcdClient.KeysAPI.Set(context.Background(), s.path(capture.ID), string(data), nil)
	return err
}

func (s *SimpleCaptureStore) SavePcap(id string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	_, err := s.etcdClient.KeysAPI.Set(context.Background(), s.pcapPath(id), base64.StdEncoding.EncodeToString(data), nil)
	return err
}

func (s *SimpleCaptureStore) GetPcap(id string) ([]byte, error) {
	resp, err := s.etcdClient.KeysAPI.Get(context.Background(), s.pcapPath(id), nil)
	if err != nil {
		if e, ok := err.(etcd.Error); ok && e.Code == etcd.ErrorCodeKeyNotFound {
			return nil, rest.ErrNotFound
		}
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(resp.Node.Value)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *SimpleCaptureStore) Get(id string) (*SimpleCapture, error) {
	resp, err := s.etcdClient.KeysAPI.Get(context.Background(), s.path(id), nil)
	if err != nil {
		if e, ok := err.(etcd.Error); ok && e.Code == etcd.ErrorCodeKeyNotFound {
			return nil, rest.ErrNotFound
		}
		return nil, err
	}
	var capture SimpleCapture
	if err := json.Unmarshal([]byte(resp.Node.Value), &capture); err != nil {
		return nil, err
	}
	return &capture, nil
}

func (s *SimpleCaptureStore) List() ([]*SimpleCapture, error) {
	resp, err := s.etcdClient.KeysAPI.Get(context.Background(), simpleCapturePrefix, &etcd.GetOptions{Recursive: true})
	if err != nil {
		if e, ok := err.(etcd.Error); ok && e.Code == etcd.ErrorCodeKeyNotFound {
			return nil, nil
		}
		return nil, err
	}

	captures := []*SimpleCapture{}
	for _, node := range resp.Node.Nodes {
		if node.Dir {
			continue
		}
		var capture SimpleCapture
		if err := json.Unmarshal([]byte(node.Value), &capture); err != nil {
			logging.GetLogger().Warningf("Failed to unmarshal simple capture state: %s", err)
			continue
		}
		captures = append(captures, &capture)
	}
	return captures, nil
}

func (a *SimpleCaptureAPI) writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(status)
	if payload != nil {
		_ = json.NewEncoder(w).Encode(payload)
	}
}

func (a *SimpleCaptureAPI) writeError(w http.ResponseWriter, status int, message string) {
	a.writeJSON(w, status, map[string]string{"error": message})
}

func (a *SimpleCaptureAPI) findTarget(req SimpleCaptureRequest) (*graph.Node, error) {
	if strings.TrimSpace(req.NodeTID) != "" {
		nodes := a.graph.GetNodes(graph.Metadata{"TID": strings.TrimSpace(req.NodeTID)})
		if len(nodes) == 1 {
			return nodes[0], nil
		}
		if len(nodes) > 1 {
			return nil, fmt.Errorf("multiple nodes matched nodeTID")
		}
	}

	if strings.TrimSpace(req.NodeID) != "" {
		if node := a.graph.GetNode(graph.Identifier(strings.TrimSpace(req.NodeID))); node != nil {
			return node, nil
		}
	}

	return nil, rest.ErrNotFound
}

func metadataString(node *graph.Node, key string) string {
	value, _ := node.GetFieldString(key)
	return value
}

func (a *SimpleCaptureAPI) validateTarget(node *graph.Node) (string, error) {
	nodeType := strings.ToLower(metadataString(node, "Type"))
	manager := strings.ToLower(metadataString(node, "Manager"))
	if manager == "k8s" {
		return nodeType, fmt.Errorf("Kubernetes logical resources cannot be captured directly")
	}
	if metadataString(node, "TID") == "" {
		return nodeType, fmt.Errorf("target node has no TID")
	}

	disallowed := map[string]struct{}{
		"host": {}, "libvirt": {}, "switch": {}, "switchport": {}, "system": {}, "tuntap": {},
	}
	if _, found := disallowed[nodeType]; found {
		return nodeType, fmt.Errorf("%s node is not a packet capture target", nodeType)
	}
	if !probes.IsCaptureAllowed(nodeType) {
		return nodeType, fmt.Errorf("capture is not supported for node type %s", nodeType)
	}
	return nodeType, nil
}

func simpleCaptureBPF(req SimpleCaptureRequest) (string, error) {
	switch strings.ToLower(strings.TrimSpace(req.FilterPreset)) {
	case "", "all":
		return "", nil
	case "ssh":
		return "tcp port 22", nil
	case "web":
		return "tcp port 80 or tcp port 443", nil
	case "dns":
		return "udp port 53 or tcp port 53", nil
	case "custom":
		return strings.TrimSpace(req.BPF), nil
	default:
		return "", fmt.Errorf("unsupported filter preset %s", req.FilterPreset)
	}
}

func normalizeSimpleCaptureRequest(req *SimpleCaptureRequest) {
	if req.Scope == "" {
		req.Scope = "selected"
	}
	if req.DurationSeconds == 0 {
		req.DurationSeconds = 30
	}
	if req.FilterPreset == "" {
		req.FilterPreset = "all"
	}
	if req.LayerKeyMode == "" {
		req.LayerKeyMode = "L3"
	}
}

func (a *SimpleCaptureAPI) buildCapture(req SimpleCaptureRequest, node *graph.Node, nodeType string) (*types.Capture, error) {
	captureType, err := probes.ProbeTypeForNode(nodeType, strings.TrimSpace(req.CaptureType))
	if err != nil {
		return nil, err
	}
	if captureType == "" {
		return nil, fmt.Errorf("capture is not supported for node type %s", nodeType)
	}

	bpf, err := simpleCaptureBPF(req)
	if err != nil {
		return nil, err
	}
	if bpf != "" && !probes.CheckProbeCapabilities(captureType, probes.BPFCapability) {
		return nil, fmt.Errorf("%s capture doesn't support BPF filtering", captureType)
	}
	if req.RawPacketLimit != 0 && !probes.CheckProbeCapabilities(captureType, probes.RawPacketsCapability) {
		return nil, fmt.Errorf("%s capture doesn't support raw packet capture", captureType)
	}
	if req.ExtraTCPMetric && !probes.CheckProbeCapabilities(captureType, probes.ExtraTCPMetricCapability) {
		return nil, fmt.Errorf("%s capture doesn't support extra TCP metrics capture", captureType)
	}

	tid := metadataString(node, "TID")
	name := metadataString(node, "Name")
	if name == "" {
		name = tid
	}

	capture := types.NewCapture(fmt.Sprintf("G.V().Has('TID', '%s')", tid), bpf)
	capture.Name = "Simple capture: " + name
	capture.Description = "Created by Netdive simple capture wizard"
	capture.Type = captureType
	capture.LayerKeyMode = req.LayerKeyMode
	capture.RawPacketLimit = req.RawPacketLimit
	capture.HeaderSize = req.HeaderSize
	capture.ExtraTCPMetric = req.ExtraTCPMetric
	capture.IPDefrag = req.IPDefrag
	capture.ReassembleTCP = req.ReassembleTCP
	return capture, nil
}

func (a *SimpleCaptureAPI) create(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if r.Method != http.MethodPost {
		a.writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	var req SimpleCaptureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	normalizeSimpleCaptureRequest(&req)

	if _, ok := simpleCaptureDurations[req.DurationSeconds]; !ok {
		a.writeError(w, http.StatusBadRequest, "durationSeconds must be one of 30, 60, 180")
		return
	}
	if req.Scope != "selected" && req.Scope != "related" && req.Scope != "all" {
		a.writeError(w, http.StatusBadRequest, "scope must be selected, related or all")
		return
	}

	a.graph.RLock()
	node, err := a.findTarget(req)
	a.graph.RUnlock()
	if err != nil {
		if errors.Is(err, rest.ErrNotFound) {
			a.writeError(w, http.StatusNotFound, "target node not found")
			return
		}
		a.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	nodeType, err := a.validateTarget(node)
	if err != nil {
		a.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	capture, err := a.buildCapture(req, node, nodeType)
	if err != nil {
		a.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := a.captureAPI.Create(capture, nil); err != nil {
		if errors.Is(err, rest.ErrDuplicatedResource) {
			a.writeError(w, http.StatusConflict, "duplicated capture")
			return
		}
		a.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	now := time.Now().UTC()
	simpleID, _ := uuid.NewV4()
	target := SimpleCaptureTarget{
		NodeTID: metadataString(node, "TID"),
		NodeID:  string(node.ID),
		Name:    metadataString(node, "Name"),
		Type:    nodeType,
	}
	simpleCapture := &SimpleCapture{
		ID:        simpleID.String(),
		CaptureID: capture.GetID(),
		Status:    "running",
		StartedAt: now,
		ExpiresAt: now.Add(time.Duration(req.DurationSeconds) * time.Second),
		Request:   req,
		Target:    target,
	}

	if err := a.store.Save(simpleCapture); err != nil {
		_ = a.captureAPI.Delete(capture.GetID())
		a.writeError(w, http.StatusInternalServerError, "failed to save simple capture state")
		return
	}
	a.schedule(simpleCapture)
	a.writeJSON(w, http.StatusAccepted, simpleCapture)
}

func (a *SimpleCaptureAPI) get(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	id := strings.TrimPrefix(r.URL.Path, "/api/simple-capture/")
	if strings.HasSuffix(id, "/download") {
		a.download(strings.TrimSuffix(id, "/download"), w, r)
		return
	}
	if id == "" {
		a.writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	capture, err := a.store.Get(id)
	if err != nil {
		if errors.Is(err, rest.ErrNotFound) {
			a.writeError(w, http.StatusNotFound, "simple capture not found")
			return
		}
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, capture)
}

func (a *SimpleCaptureAPI) download(id string, w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	if r.Method != http.MethodGet {
		a.writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	if id == "" {
		a.writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	capture, err := a.store.Get(id)
	if err != nil {
		if errors.Is(err, rest.ErrNotFound) {
			a.writeError(w, http.StatusNotFound, "simple capture not found")
			return
		}
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if capture.Status == "running" {
		a.writeError(w, http.StatusConflict, "capture is still running")
		return
	}
	if capture.Request.RawPacketLimit == 0 {
		a.writeError(w, http.StatusConflict, "raw packet capture was disabled")
		return
	}
	if capture.Target.NodeTID == "" {
		a.writeError(w, http.StatusBadRequest, "capture target has no TID")
		return
	}

	pcapData, err := a.store.GetPcap(capture.ID)
	if err != nil {
		if !errors.Is(err, rest.ErrNotFound) {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		// Compatibility fallback for captures created before snapshot persistence.
		pcapData, err = a.renderCapturePCAP(capture)
		if err != nil {
			a.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if len(pcapData) == 0 {
		a.writeError(w, http.StatusNotFound, "downloadable packet data is not available")
		return
	}

	filename := fmt.Sprintf("netdive-capture-%s-%s.pcap", capture.Target.Name, capture.ID)
	filename = strings.ReplaceAll(filename, "/", "-")
	w.Header().Set("Content-Type", "application/vnd.tcpdump.pcap")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pcapData)
}

func (a *SimpleCaptureAPI) renderCapturePCAP(capture *SimpleCapture) ([]byte, error) {
	query := fmt.Sprintf("G.V().Has('TID', '%s').Flows().RawPackets()", capture.Target.NodeTID)
	ts, err := a.gremlinParser.Parse(strings.NewReader(query))
	if err != nil {
		return nil, err
	}

	result, err := ts.Exec(a.graph, true)
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	if err := pcapMarshaller(result, &buffer); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (a *SimpleCaptureAPI) delete(w http.ResponseWriter, r *auth.AuthenticatedRequest) {
	id := strings.TrimPrefix(r.URL.Path, "/api/simple-capture/")
	if id == "" {
		a.writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	capture, err := a.stop(id)
	if err != nil {
		if errors.Is(err, rest.ErrNotFound) {
			a.writeError(w, http.StatusNotFound, "simple capture not found")
			return
		}
		a.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeJSON(w, http.StatusOK, capture)
}

func (a *SimpleCaptureAPI) schedule(capture *SimpleCapture) {
	if capture.Status != "running" {
		return
	}
	delay := time.Until(capture.ExpiresAt)
	if delay < 0 {
		delay = 0
	}

	a.mutex.Lock()
	if timer, ok := a.timers[capture.ID]; ok {
		timer.Stop()
	}
	a.timers[capture.ID] = time.AfterFunc(delay, func() {
		if _, err := a.stop(capture.ID); err != nil {
			logging.GetLogger().Warningf("Failed to auto stop simple capture %s: %s", capture.ID, err)
		}
	})
	a.mutex.Unlock()
}

func (a *SimpleCaptureAPI) stop(id string) (*SimpleCapture, error) {
	capture, err := a.store.Get(id)
	if err != nil {
		return nil, err
	}
	if capture.Status == "completed" || capture.Status == "expired" {
		return capture, nil
	}

	if capture.Request.RawPacketLimit != 0 && capture.Target.NodeTID != "" {
		if data, err := a.renderCapturePCAP(capture); err != nil {
			logging.GetLogger().Warningf("Failed to snapshot simple capture %s pcap: %s", capture.ID, err)
		} else if len(data) > 0 {
			if err := a.store.SavePcap(capture.ID, data); err != nil {
				logging.GetLogger().Warningf("Failed to save simple capture %s pcap snapshot: %s", capture.ID, err)
			}
		}
	}

	if err := a.captureAPI.Delete(capture.CaptureID); err != nil && !errors.Is(err, rest.ErrNotFound) {
		capture.Status = "delete_failed"
		capture.Message = err.Error()
		_ = a.store.Save(capture)
		return capture, err
	}

	now := time.Now().UTC()
	capture.DeletedAt = &now
	if now.After(capture.ExpiresAt) || now.Equal(capture.ExpiresAt) {
		capture.Status = "expired"
	} else {
		capture.Status = "completed"
	}
	capture.Message = ""
	if err := a.store.Save(capture); err != nil {
		return capture, err
	}

	a.mutex.Lock()
	if timer, ok := a.timers[id]; ok {
		timer.Stop()
		delete(a.timers, id)
	}
	a.mutex.Unlock()
	return capture, nil
}

func (a *SimpleCaptureAPI) startSweeper() {
	captures, err := a.store.List()
	if err != nil {
		logging.GetLogger().Warningf("Failed to load simple capture states: %s", err)
		return
	}
	for _, capture := range captures {
		if capture.Status != "running" {
			continue
		}
		a.schedule(capture)
	}
}

func RegisterSimpleCaptureAPI(httpServer *shttp.Server, graph *graph.Graph, gremlinParser *traversal.GremlinTraversalParser, captureAPI *CaptureAPIHandler, authBackend shttp.AuthenticationBackend) *SimpleCaptureAPI {
	api := &SimpleCaptureAPI{
		graph:         graph,
		gremlinParser: gremlinParser,
		captureAPI:    captureAPI,
		store:         NewSimpleCaptureStore(captureAPI.EtcdClient),
		timers:        make(map[string]*time.Timer),
	}

	routes := []shttp.Route{
		{
			Name:        "SimpleCaptureCreate",
			Method:      "POST",
			Path:        "/api/simple-capture",
			HandlerFunc: api.create,
		},
		{
			Name:        "SimpleCaptureGet",
			Method:      "GET",
			Path:        shttp.PathPrefix("/api/simple-capture/"),
			HandlerFunc: api.get,
		},
		{
			Name:        "SimpleCaptureDelete",
			Method:      "DELETE",
			Path:        shttp.PathPrefix("/api/simple-capture/"),
			HandlerFunc: api.delete,
		},
	}
	httpServer.RegisterRoutes(routes, authBackend)
	api.startSweeper()
	return api
}
