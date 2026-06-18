package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewUsesProductionAPIRegistryByDefault(t *testing.T) {
	client := New("")
	if client.BaseURL != "https://api.greprules.io" {
		t.Fatalf("unexpected default registry URL: %q", client.BaseURL)
	}
}

func TestClientFetchesPacksManifestAndTarball(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/packs":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"packs":[{"slug":"go-security","name":"Go","summary":"","source_type":"community","total_rules":1,"download_count":0,"last_commit":"","languages":["go"],"selection":{"kind":"language","languages":["go"],"frameworks":[],"source_types":[],"tags":[]},"validation_status":"verified","manifest_url":"/api/packs/go-security/manifest.json","download_url":"/api/packs/go-security/latest.tar.gz","created_at":"","updated_at":""}]}`))
		case "/api/packs/go-security/manifest.json":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"schema_version":1,"slug":"go-security","build_id":"build-1","total_rules":1,"languages":["go"],"rules":[]}`))
		case "/api/packs/go-security/latest.tar.gz":
			w.Header().Set("content-type", "application/gzip")
			_, _ = w.Write([]byte("fake"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := New(server.URL)
	packs, err := client.ListPacks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 1 || packs[0].Slug != "go-security" {
		t.Fatalf("unexpected packs: %#v", packs)
	}
	if packs[0].Selection.Kind != "language" || len(packs[0].Selection.Languages) != 1 || packs[0].Selection.Languages[0] != "go" {
		t.Fatalf("unexpected pack selection: %#v", packs[0].Selection)
	}
	_, manifest, err := client.FetchManifest(context.Background(), "go-security")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.BuildID != "build-1" {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	body, err := client.DownloadPack(context.Background(), "go-security")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "fake" {
		t.Fatalf("unexpected tarball body: %q", string(body))
	}
}
