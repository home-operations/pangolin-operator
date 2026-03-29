package pangolin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func apiResponse(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	type envelope struct {
		Data    any    `json:"data"`
		Success bool   `json:"success"`
		Message string `json:"message"`
		Status  int    `json:"status"`
	}
	if err := json.NewEncoder(w).Encode(envelope{Data: data, Success: true, Status: 200}); err != nil {
		t.Fatalf("failed to write test response: %v", err)
	}
}

func apiError(t *testing.T, w http.ResponseWriter, statusCode int, msg string) {
	t.Helper()
	w.WriteHeader(statusCode)
	w.Header().Set("Content-Type", "application/json")
	type envelope struct {
		Success bool   `json:"success"`
		Error   bool   `json:"error"`
		Message string `json:"message"`
		Status  int    `json:"status"`
	}
	_ = json.NewEncoder(w).Encode(envelope{Success: false, Error: true, Message: msg, Status: statusCode})
}

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := NewClient(Credentials{
		Endpoint: srv.URL,
		APIKey:   "test-key",
		OrgID:    "test-org",
	})
	return client
}

func TestNewClient(t *testing.T) {
	c := NewClient(Credentials{
		Endpoint: "https://pangolin.example.com/",
		APIKey:   "key",
		OrgID:    "org1",
	})
	if c.endpoint != "https://pangolin.example.com" {
		t.Errorf("expected trailing slash stripped, got %q", c.endpoint)
	}
	if c.apiKey != "key" {
		t.Errorf("unexpected apiKey %q", c.apiKey)
	}
	if c.orgID != "org1" {
		t.Errorf("unexpected orgID %q", c.orgID)
	}
}

func TestPickSiteDefaults(t *testing.T) {
	want := PickSiteDefaultsResponse{
		NewtID:        "newt-abc",
		NewtSecret:    "secret-xyz",
		ClientAddress: "100.90.128.0",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/test-org/pick-site-defaults", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing or wrong Authorization header: %s", r.Header.Get("Authorization"))
		}
		apiResponse(t, w, want)
	})

	c := newTestClient(t, mux)
	got, err := c.PickSiteDefaults(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.NewtID != want.NewtID || got.NewtSecret != want.NewtSecret || got.ClientAddress != want.ClientAddress {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestPickSiteDefaults_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/test-org/pick-site-defaults", func(w http.ResponseWriter, r *http.Request) {
		apiError(t, w, 500, "internal server error")
	})

	c := newTestClient(t, mux)
	_, err := c.PickSiteDefaults(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateSite(t *testing.T) {
	want := CreateSiteResponse{SiteID: 42, NiceID: "site-42"}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/test-org/site", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		var req CreateSiteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("could not decode request body: %v", err)
		}
		if req.Name != "my-site" {
			t.Errorf("unexpected site name: %q", req.Name)
		}
		if req.Address == "" {
			t.Errorf("expected non-empty address")
		}
		apiResponse(t, w, want)
	})

	c := newTestClient(t, mux)
	got, err := c.CreateSite(context.Background(), CreateSiteRequest{
		Name:    "my-site",
		Address: "100.90.128.0",
		Type:    "newt",
		NewtID:  "nid",
		Secret:  "sec",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SiteID != want.SiteID || got.NiceID != want.NiceID {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetSite(t *testing.T) {
	want := GetSiteResponse{SiteID: 42, NiceID: "site-42", Name: "my-site", Online: false}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		apiResponse(t, w, want)
	})

	c := newTestClient(t, mux)
	got, err := c.GetSite(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SiteID != want.SiteID || got.Name != want.Name {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestUpdateSite(t *testing.T) {
	called := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var req UpdateSiteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("could not decode request body: %v", err)
		}
		if req.Name != "renamed-site" {
			t.Errorf("unexpected name: %q", req.Name)
		}
		called = true
		apiResponse(t, w, nil)
	})

	c := newTestClient(t, mux)
	if err := c.UpdateSite(context.Background(), 42, UpdateSiteRequest{Name: "renamed-site"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("update handler was not called")
	}
}

func TestDeleteSite(t *testing.T) {
	deleted := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		deleted = true
		apiResponse(t, w, nil)
	})

	c := newTestClient(t, mux)
	if err := c.DeleteSite(context.Background(), 42); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Error("delete handler was not called")
	}
}

func TestCreateResource(t *testing.T) {
	want := CreateResourceResponse{ResourceID: 7, NiceID: "res-7", FullDomain: "app.example.com"}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/test-org/resource", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		apiResponse(t, w, want)
	})

	c := newTestClient(t, mux)
	got, err := c.CreateResource(context.Background(), CreateResourceRequest{
		Name:     "my-resource",
		Http:     true,
		Protocol: "tcp",
		DomainId: "dom-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ResourceID != want.ResourceID {
		t.Errorf("got resourceID %d, want %d", got.ResourceID, want.ResourceID)
	}
}

func TestGetResource(t *testing.T) {
	want := GetResourceResponse{ResourceID: 7, Name: "my-resource", FullDomain: "app.example.com"}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		apiResponse(t, w, want)
	})

	c := newTestClient(t, mux)
	got, err := c.GetResource(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ResourceID != want.ResourceID || got.Name != want.Name {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestUpdateResource(t *testing.T) {
	called := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var req UpdateResourceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("could not decode request body: %v", err)
		}
		if req.Name != "renamed-resource" {
			t.Errorf("unexpected name: %q", req.Name)
		}
		called = true
		apiResponse(t, w, nil)
	})

	c := newTestClient(t, mux)
	if err := c.UpdateResource(context.Background(), 7, UpdateResourceRequest{Name: "renamed-resource"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("update handler was not called")
	}
}

func TestDeleteResource(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resource/7", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		apiResponse(t, w, nil)
	})

	c := newTestClient(t, mux)
	if err := c.DeleteResource(context.Background(), 7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateTarget(t *testing.T) {
	want := CreateTargetResponse{TargetID: 99}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/resource/7/target", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		apiResponse(t, w, want)
	})

	c := newTestClient(t, mux)
	got, err := c.CreateTarget(context.Background(), 7, CreateTargetRequest{
		SiteID: 42,
		Ip:     "10.0.0.1",
		Port:   8080,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TargetID != want.TargetID {
		t.Errorf("got targetID %d, want %d", got.TargetID, want.TargetID)
	}
}

func TestDeleteTarget(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/target/99", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		apiResponse(t, w, nil)
	})

	c := newTestClient(t, mux)
	if err := c.DeleteTarget(context.Background(), 99); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateSiteResource(t *testing.T) {
	want := CreateSiteResourceResponse{SiteResourceID: 55, NiceID: "sres-55"}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/test-org/site-resource", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		var req CreateSiteResourceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("could not decode request body: %v", err)
		}
		if req.RoleIds == nil || req.UserIds == nil || req.ClientIds == nil {
			t.Errorf("roleIds, userIds, and clientIds must not be nil (API requires them)")
		}
		apiResponse(t, w, want)
	})

	c := newTestClient(t, mux)
	got, err := c.CreateSiteResource(context.Background(), CreateSiteResourceRequest{
		Name:               "priv-res",
		SiteID:             42,
		Mode:               "host",
		Destination:        "10.0.0.5",
		TcpPortRangeString: "*",
		UdpPortRangeString: "",
		RoleIds:            []int{},
		UserIds:            []string{},
		ClientIds:          []int{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SiteResourceID != want.SiteResourceID {
		t.Errorf("got siteResourceID %d, want %d", got.SiteResourceID, want.SiteResourceID)
	}
}

func TestGetSiteResource(t *testing.T) {
	want := GetSiteResourceResponse{SiteResourceID: 55, Name: "priv-res", Mode: "host"}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site-resource/55", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		apiResponse(t, w, want)
	})

	c := newTestClient(t, mux)
	got, err := c.GetSiteResource(context.Background(), 55)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.SiteResourceID != want.SiteResourceID || got.Name != want.Name {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestUpdateSiteResource(t *testing.T) {
	called := false

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site-resource/55", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var req UpdateSiteResourceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("could not decode request body: %v", err)
		}
		if req.Name != "renamed-priv" {
			t.Errorf("unexpected name: %q", req.Name)
		}
		called = true
		apiResponse(t, w, nil)
	})

	c := newTestClient(t, mux)
	if err := c.UpdateSiteResource(context.Background(), 55, UpdateSiteResourceRequest{
		SiteID:    42,
		UserIds:   []string{},
		RoleIds:   []int{},
		ClientIds: []int{},
		Name:      "renamed-priv",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("update handler was not called")
	}
}

func TestDeleteSiteResource(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/site-resource/55", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		apiResponse(t, w, nil)
	})

	c := newTestClient(t, mux)
	if err := c.DeleteSiteResource(context.Background(), 55); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDo_AuthorizationHeader(t *testing.T) {
	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/org/test-org/pick-site-defaults", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		apiResponse(t, w, PickSiteDefaultsResponse{NewtID: "x", NewtSecret: "y"})
	})

	c := newTestClient(t, mux)
	_, _ = c.PickSiteDefaults(context.Background())
	if gotAuth != "Bearer test-key" {
		t.Errorf("expected 'Bearer test-key', got %q", gotAuth)
	}
}
