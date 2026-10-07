package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/skydive-project/skydive/common"
	"github.com/skydive-project/skydive/config"
)

type kubernetesListTestCredentials struct {
	common.MoldCredentialsStore
}

func (kubernetesListTestCredentials) LoadMoldAPICredentials(context.Context) (string, string, bool, error) {
	return "test-api-key", "test-secret", true, nil
}

func TestListMoldKubernetesClustersIncludesOtherOwners(t *testing.T) {
	mold := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("command") != "listKubernetesClusters" || query.Get("response") != "json" {
			t.Error("unexpected Mold list request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Mold defaults to the caller's own resources, even for an admin.
		if query.Get("listall") != "true" {
			_, _ = w.Write([]byte(`{"listkubernetesclustersresponse":{"count":0}}`))
			return
		}
		_, _ = w.Write([]byte(`{"listkubernetesclustersresponse":{"count":1,"kubernetescluster":[{"id":"other-owner-cluster","name":"shared-cluster","state":"Running","account":"team-user","domain":"ROOT","project":"team-project"}]}}`))
	}))
	defer mold.Close()

	previousEndpoint := config.GetString("mold.api.endpoint")
	config.Set("mold.api.endpoint", mold.URL)
	common.SetMoldAPICredentialsStore(kubernetesListTestCredentials{})
	t.Cleanup(func() {
		config.Set("mold.api.endpoint", previousEndpoint)
		common.SetMoldAPICredentialsStore(nil)
	})

	clusters, err := listMoldKubernetesClusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 || clusters[0].ID != "other-owner-cluster" {
		t.Fatalf("other owner's authorized cluster missing: %#v", clusters)
	}
	if clusters[0].AccountName != "team-user" || clusters[0].DomainName != "ROOT" || clusters[0].ProjectName != "team-project" {
		t.Fatalf("cluster ownership was not preserved: %#v", clusters[0])
	}
}

func TestParseMoldKubernetesClustersWithoutOwnership(t *testing.T) {
	clusters, err := parseMoldKubernetesClusters([]byte(`{"listkubernetesclustersresponse":{"kubernetescluster":[{"id":"legacy","name":"legacy-cluster"}]}}`))
	if err != nil || len(clusters) != 1 {
		t.Fatalf("legacy response failed: clusters=%#v err=%v", clusters, err)
	}
	if clusters[0].AccountName != "" || clusters[0].DomainName != "" || clusters[0].ProjectName != "" {
		t.Fatalf("missing ownership must remain unknown: %#v", clusters[0])
	}
}
