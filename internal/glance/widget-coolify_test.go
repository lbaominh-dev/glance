package glance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSelectCoolifyPrimaryURL(t *testing.T) {
	tests := []struct {
		fqdn string
		want string
	}{
		{"", ""},
		{"https://app.example.com/", "https://app.example.com"},
		{"http://internal.local:8080, https://app.example.com", "https://app.example.com"},
		{"app.example.com", "https://app.example.com"},
		{"http://only-http.local:3000/", "http://only-http.local:3000"},
	}

	for _, tc := range tests {
		got := selectCoolifyPrimaryURL(tc.fqdn)
		if got != tc.want {
			t.Errorf("selectCoolifyPrimaryURL(%q) = %q, want %q", tc.fqdn, got, tc.want)
		}
	}
}

func TestBuildCoolifyCheckURL(t *testing.T) {
	tests := []struct {
		base string
		path string
		want string
	}{
		{"", "/health", ""},
		{"https://app.example.com", "", "https://app.example.com"},
		{"https://app.example.com", "/", "https://app.example.com"},
		{"https://app.example.com", "/api/health", "https://app.example.com/api/health"},
		{"https://app.example.com", "healthz", "https://app.example.com/healthz"},
	}

	for _, tc := range tests {
		got := buildCoolifyCheckURL(tc.base, tc.path)
		if got != tc.want {
			t.Errorf("buildCoolifyCheckURL(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}
}

func TestFinalizeAndSortCoolifyServiceItems(t *testing.T) {
	items := []coolifyServiceItem{
		{Name: "zebra-api", CoolifyState: "running · healthy", HasHTTPCheck: true, HTTPOK: true},
		{Name: "alpha-web", CoolifyState: "running · healthy", HasHTTPCheck: true, HTTPOK: false},
		{Name: "beta-worker", CoolifyState: "restarting", HasHTTPCheck: false},
		{Name: "db-main", CoolifyState: "exited · unhealthy", HasHTTPCheck: false},
	}

	for i := range items {
		finalizeCoolifyServiceItem(&items[i])
	}
	sortCoolifyServiceItems(items)

	wantOrder := []struct {
		name  string
		style string
	}{
		{"alpha-web", "warn"},
		{"db-main", "warn"},
		{"beta-worker", "muted"},
		{"zebra-api", "ok"},
	}

	for i, want := range wantOrder {
		if items[i].Name != want.name || items[i].StateStyle != want.style {
			t.Errorf("items[%d] = (%q, %q), want (%q, %q)", i, items[i].Name, items[i].StateStyle, want.name, want.style)
		}
	}
}

func TestCoolifyWidgetUnconfiguredDoesNotFail(t *testing.T) {
	t.Setenv("COOLIFY_URL", "")
	t.Setenv("COOLIFY_API_TOKEN", "")

	w := &coolifyWidget{}
	if err := w.initialize(); err != nil {
		t.Fatalf("initialize() returned unexpected error: %v", err)
	}

	w.update(context.Background())

	if !w.ContentAvailable || w.Error != nil {
		t.Fatalf("expected ContentAvailable=true and Error=nil, got ContentAvailable=%v Error=%v", w.ContentAvailable, w.Error)
	}
	if w.Configured {
		t.Fatal("expected Configured=false when COOLIFY_URL is empty")
	}

	rendered := string(w.Render())
	if !strings.Contains(rendered, "Coolify API not configured") {
		t.Errorf("expected setup prompt in rendered HTML, got: %s", rendered)
	}
}

func TestCoolifyWidgetWithMockAPIAndHealthChecks(t *testing.T) {
	healthyAppSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer healthyAppSrv.Close()

	failingSvcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer failingSvcSrv.Close()

	coolifyAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}

		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/version":
			_, _ = w.Write([]byte("4.0.0-beta.400"))
		case "/api/v1/servers":
			_ = json.NewEncoder(w).Encode([]coolifyServerJSON{
				{
					UUID: "srv-1",
					Name: "vps-main",
					IP:   "10.0.0.1",
					Settings: struct {
						IsReachable bool `json:"is_reachable"`
						IsUsable    bool `json:"is_usable"`
					}{IsReachable: true, IsUsable: true},
				},
			})
		case "/api/v1/applications":
			_ = json.NewEncoder(w).Encode([]coolifyApplicationJSON{
				{
					UUID:               "app-1",
					Name:               "glance-api",
					FQDN:               healthyAppSrv.URL,
					Status:             "running:healthy",
					HealthCheckEnabled: true,
					HealthCheckPath:    "/healthz",
					GitRepository:      "user/glance-api",
					GitBranch:          "main",
				},
			})
		case "/api/v1/services":
			_ = json.NewEncoder(w).Encode([]coolifyServiceJSON{
				{
					UUID:   "svc-1",
					Name:   "uptime-kuma",
					Status: "running:healthy",
					Applications: []struct {
						UUID   string `json:"uuid"`
						Name   string `json:"name"`
						FQDN   string `json:"fqdn"`
						Status string `json:"status"`
					}{
						{
							UUID:   "sub-1",
							Name:   "uptime-kuma",
							FQDN:   failingSvcSrv.URL,
							Status: "running:healthy",
						},
					},
				},
			})
		case "/api/v1/databases":
			_ = json.NewEncoder(w).Encode([]coolifyDatabaseJSON{
				{
					UUID:   "db-1",
					Name:   "postgres-prod",
					Type:   "standalone-postgresql",
					Status: "running:healthy",
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer coolifyAPI.Close()

	t.Setenv("COOLIFY_URL", coolifyAPI.URL)
	t.Setenv("COOLIFY_API_TOKEN", "test-token")

	w := &coolifyWidget{}
	if err := w.initialize(); err != nil {
		t.Fatalf("initialize() failed: %v", err)
	}

	w.update(context.Background())

	if !w.ContentAvailable || w.Error != nil {
		t.Fatalf("unexpected error state: ContentAvailable=%v Error=%v", w.ContentAvailable, w.Error)
	}
	if !w.APIHealthy || w.Version != "4.0.0-beta.400" {
		t.Fatalf("unexpected Coolify health/version: healthy=%v version=%q", w.APIHealthy, w.Version)
	}
	if w.OnlineServers != 1 || len(w.Servers) != 1 {
		t.Fatalf("expected 1 online server, got %d/%d", w.OnlineServers, len(w.Servers))
	}
	if w.HealthyServices != 1 || w.TotalServices != 2 {
		t.Fatalf("expected 1/2 healthy services, got %d/%d", w.HealthyServices, w.TotalServices)
	}
	if w.RunningDatabases != 1 || w.TotalDatabases != 1 {
		t.Fatalf("expected 1/1 running DBs, got %d/%d", w.RunningDatabases, w.TotalDatabases)
	}

	// Failing service should sort first
	if len(w.Services) != 3 {
		t.Fatalf("expected 3 items (app + service + db), got %d", len(w.Services))
	}
	if w.Services[0].Name != "uptime-kuma" || w.Services[0].StateStyle != "warn" {
		t.Errorf("expected failing uptime-kuma first with warn state, got %+v", w.Services[0])
	}

	rendered := string(w.Render())
	for _, needle := range []string{"vps-main", "glance-api", "uptime-kuma", "postgres-prod", "200 ·", "503 ·"} {
		if !strings.Contains(rendered, needle) {
			t.Errorf("expected rendered HTML to contain %q", needle)
		}
	}
}

func TestCoolifyWidgetMultipleInstancesAndShowFailingOnly(t *testing.T) {
	okAppSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer okAppSrv.Close()

	badAppSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer badAppSrv.Close()

	makeCoolifyMock := func(serverName, appName, appURL string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/health":
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("OK"))
			case "/api/v1/servers":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]coolifyServerJSON{{UUID: "s", Name: serverName, Settings: struct {
					IsReachable bool `json:"is_reachable"`
					IsUsable    bool `json:"is_usable"`
				}{IsReachable: true, IsUsable: true}}})
			case "/api/v1/applications":
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]coolifyApplicationJSON{{UUID: "a", Name: appName, FQDN: appURL, Status: "running:healthy"}})
			case "/api/v1/services":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("[]"))
			case "/api/v1/databases":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte("[]"))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
	}

	c1 := makeCoolifyMock("node-sg", "web-sg", okAppSrv.URL)
	defer c1.Close()
	c2 := makeCoolifyMock("node-eu", "api-eu", badAppSrv.URL)
	defer c2.Close()

	w := &coolifyWidget{
		ShowFailingOnly: true,
		Instances: []coolifyInstanceConfig{
			{Name: "VPS-1", URL: c1.URL, Token: "t1"},
			{Name: "VPS-2", URL: c2.URL, Token: "t2"},
		},
	}
	if err := w.initialize(); err != nil {
		t.Fatalf("initialize() failed: %v", err)
	}

	w.update(context.Background())

	if !w.ContentAvailable || w.Error != nil {
		t.Fatalf("unexpected error: %v", w.Error)
	}
	if w.HealthyInstances != 2 || w.TotalInstances != 2 {
		t.Fatalf("expected 2/2 healthy instances, got %d/%d", w.HealthyInstances, w.TotalInstances)
	}
	if w.OnlineServers != 2 || len(w.Servers) != 2 {
		t.Fatalf("expected 2 online servers, got %d/%d", w.OnlineServers, len(w.Servers))
	}
	if w.HealthyServices != 1 || w.TotalServices != 2 {
		t.Fatalf("expected 1/2 healthy services, got %d/%d", w.HealthyServices, w.TotalServices)
	}
	if len(w.Services) != 1 || w.Services[0].Name != "api-eu" {
		t.Fatalf("expected only failing api-eu when ShowFailingOnly=true, got %+v", w.Services)
	}
	if !strings.Contains(w.Services[0].Subtitle, "VPS-2") {
		t.Errorf("expected multi-instance subtitle to include instance scope VPS-2, got %q", w.Services[0].Subtitle)
	}
}

func TestCoolifyWidgetSentinelMetricsAutoDiscovery(t *testing.T) {
	sentinelSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sentinel-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/cpu/history":
			_ = json.NewEncoder(w).Encode([]coolifySentinelPercentPoint{
				{Time: "2026-11-26T05:00:00Z", Percent: 12.4},
				{Time: "2026-11-26T05:01:00Z", Percent: 24.6},
			})
		case "/api/memory/history":
			_ = json.NewEncoder(w).Encode([]coolifySentinelPercentPoint{
				{Time: "2026-11-26T05:01:00Z", Percent: 58.3},
			})
		case "/api/disk/history":
			_ = json.NewEncoder(w).Encode([]coolifySentinelDiskPoint{
				{Mount: "/", UsedPercent: 86.2},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer sentinelSrv.Close()

	coolifyAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		if r.Header.Get("Authorization") != "Bearer coolify-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/servers":
			_ = json.NewEncoder(w).Encode([]coolifyServerJSON{{
				UUID: "srv-uuid-1",
				Name: "prod-vps",
				IP:   "10.0.0.10",
				Settings: struct {
					IsReachable bool `json:"is_reachable"`
					IsUsable    bool `json:"is_usable"`
				}{IsReachable: true, IsUsable: true},
			}})
		case "/api/v1/servers/srv-uuid-1/sentinel":
			_ = json.NewEncoder(w).Encode(coolifyServerSentinelJSON{
				IsSentinelEnabled: true,
				IsMetricsEnabled:  true,
				SentinelToken:     "sentinel-secret",
				SentinelCustomURL: sentinelSrv.URL,
			})
		case "/api/v1/applications", "/api/v1/services", "/api/v1/databases":
			_, _ = w.Write([]byte("[]"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer coolifyAPI.Close()

	t.Setenv("COOLIFY_URL", coolifyAPI.URL)
	t.Setenv("COOLIFY_API_TOKEN", "coolify-token")
	t.Setenv("COOLIFY_SENTINEL_URL", "")
	t.Setenv("COOLIFY_SENTINEL_TOKEN", "")

	w := &coolifyWidget{}
	if err := w.initialize(); err != nil {
		t.Fatalf("initialize() failed: %v", err)
	}

	w.update(context.Background())

	if len(w.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(w.Servers))
	}
	srv := w.Servers[0]
	if !srv.HasMetrics || !srv.HasCPU || !srv.HasMemory || !srv.HasDisk {
		t.Fatalf("expected CPU, Memory, and Disk metrics to be populated, got %+v", srv)
	}
	if srv.CPUPercent != 25 || srv.CPUHigh {
		t.Errorf("expected CPUPercent=25 CPUHigh=false, got %d high=%v", srv.CPUPercent, srv.CPUHigh)
	}
	if srv.MemoryPercent != 58 || srv.MemoryHigh {
		t.Errorf("expected MemoryPercent=58 MemoryHigh=false, got %d high=%v", srv.MemoryPercent, srv.MemoryHigh)
	}
	if srv.DiskPercent != 86 || !srv.DiskHigh {
		t.Errorf("expected DiskPercent=86 DiskHigh=true, got %d high=%v", srv.DiskPercent, srv.DiskHigh)
	}

	rendered := string(w.Render())
	for _, needle := range []string{"CPU 25%", "RAM 58%", "Disk 86%"} {
		if !strings.Contains(rendered, needle) {
			t.Errorf("expected rendered HTML to contain %q", needle)
		}
	}
}
