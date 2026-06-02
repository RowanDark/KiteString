package ksfile

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/RowanDark/kitestring/pkg/proute"
	"google.golang.org/protobuf/proto"
)

func sampleRoutes(n int) []proute.Route {
	routes := make([]proute.Route, n)
	for i := range routes {
		routes[i] = proute.Route{
			Method:      "POST",
			Path:        fmt.Sprintf("/api/v1/resource/%d", i),
			ContentType: "application/json",
			KSUID:       fmt.Sprintf("ksuid-%d", i),
			Headers: []proute.Crumb{
				{Key: "Authorization", Type: proute.CrumbString, Required: true, Example: "Bearer token"},
			},
			QueryParams: []proute.Crumb{
				{Key: "limit", Type: proute.CrumbInt, Required: false, Example: "20"},
			},
			BodyParams: []proute.Crumb{
				{Key: "email", Type: proute.CrumbEmail, Required: true, Example: "user@example.com"},
				{Key: "active", Type: proute.CrumbBool, Required: false},
			},
		}
	}
	return routes
}

func TestRoundTrip(t *testing.T) {
	routes := sampleRoutes(10)
	meta := KSFileMeta{
		Name:        "test wordlist",
		Description: "round-trip test",
		Source:      "unit test",
		CreatedAt:   "2024-01-01T00:00:00Z",
	}

	kf := FromRoutes(routes, meta)

	tmp, err := os.CreateTemp(t.TempDir(), "*.ks")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	if err := Write(tmp.Name(), kf); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Read(tmp.Name())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got.Version != CurrentVersion {
		t.Errorf("version: got %d, want %d", got.Version, CurrentVersion)
	}
	if got.Name != meta.Name {
		t.Errorf("name: got %q, want %q", got.Name, meta.Name)
	}
	if got.Description != meta.Description {
		t.Errorf("description: got %q, want %q", got.Description, meta.Description)
	}
	if got.Source != meta.Source {
		t.Errorf("source: got %q, want %q", got.Source, meta.Source)
	}
	if got.CreatedAt != meta.CreatedAt {
		t.Errorf("created_at: got %q, want %q", got.CreatedAt, meta.CreatedAt)
	}
	if len(got.Routes) != len(routes) {
		t.Fatalf("routes count: got %d, want %d", len(got.Routes), len(routes))
	}

	back, err := ToRoutes(got)
	if err != nil {
		t.Fatalf("ToRoutes: %v", err)
	}

	for i, r := range back {
		orig := routes[i]
		if r.Method != orig.Method {
			t.Errorf("route[%d].Method: got %q, want %q", i, r.Method, orig.Method)
		}
		if r.Path != orig.Path {
			t.Errorf("route[%d].Path: got %q, want %q", i, r.Path, orig.Path)
		}
		if r.ContentType != orig.ContentType {
			t.Errorf("route[%d].ContentType: got %q, want %q", i, r.ContentType, orig.ContentType)
		}
		if r.KSUID != orig.KSUID {
			t.Errorf("route[%d].KSUID: got %q, want %q", i, r.KSUID, orig.KSUID)
		}
		if len(r.Headers) != len(orig.Headers) {
			t.Errorf("route[%d].Headers count: got %d, want %d", i, len(r.Headers), len(orig.Headers))
		}
		if len(r.QueryParams) != len(orig.QueryParams) {
			t.Errorf("route[%d].QueryParams count: got %d, want %d", i, len(r.QueryParams), len(orig.QueryParams))
		}
		if len(r.BodyParams) != len(orig.BodyParams) {
			t.Errorf("route[%d].BodyParams count: got %d, want %d", i, len(r.BodyParams), len(orig.BodyParams))
		}
		for j, c := range r.BodyParams {
			oc := orig.BodyParams[j]
			if c.Key != oc.Key || c.Type != oc.Type || c.Required != oc.Required || c.Example != oc.Example {
				t.Errorf("route[%d].BodyParams[%d]: got %+v, want %+v", i, j, c, oc)
			}
		}
	}
}

func TestVersionMismatch(t *testing.T) {
	kf := &KSFile{
		Version: CurrentVersion + 1,
		Name:    "future",
	}

	tmp, err := os.CreateTemp(t.TempDir(), "*.ks")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	if err := Write(tmp.Name(), kf); err != nil {
		t.Fatalf("Write: %v", err)
	}

	_, err = Read(tmp.Name())
	if err == nil {
		t.Fatal("expected error for unsupported version, got nil")
	}
}

func TestCompressionSmallerThanJSON(t *testing.T) {
	routes := sampleRoutes(1000)
	meta := KSFileMeta{
		Name:      "compression test",
		CreatedAt: "2024-01-01T00:00:00Z",
	}

	kf := FromRoutes(routes, meta)

	tmp, err := os.CreateTemp(t.TempDir(), "*.ks")
	if err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	if err := Write(tmp.Name(), kf); err != nil {
		t.Fatalf("Write: %v", err)
	}

	ksInfo, err := os.Stat(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}

	jsonData, err := json.Marshal(kf)
	if err != nil {
		t.Fatal(err)
	}

	ksSize := ksInfo.Size()
	jsonSize := int64(len(jsonData))

	// Verify using raw protobuf size too
	rawProto, _ := proto.Marshal(kf)
	t.Logf(".ks size: %d bytes, JSON size: %d bytes, raw proto size: %d bytes", ksSize, jsonSize, len(rawProto))

	if ksSize >= jsonSize {
		t.Errorf(".ks file (%d bytes) is not smaller than JSON (%d bytes)", ksSize, jsonSize)
	}
}
