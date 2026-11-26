package glance

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var coolifyWidgetTemplate = mustParseTemplate("coolify.html", "widget-base.html")

type coolifyInstanceConfig struct {
	Name          string `yaml:"name"`
	URL           string `yaml:"url"`
	Token         string `yaml:"token"`
	SentinelURL   string `yaml:"sentinel-url"`
	SentinelToken string `yaml:"sentinel-token"`
	AllowInsecure bool   `yaml:"allow-insecure"`
}

type coolifyWidget struct {
	widgetBase         `yaml:",inline"`
	URL                string                  `yaml:"url"`
	Token              string                  `yaml:"token"`
	SentinelURL        string                  `yaml:"sentinel-url"`
	SentinelToken      string                  `yaml:"sentinel-token"`
	AllowInsecure      bool                    `yaml:"allow-insecure"`
	Instances          []coolifyInstanceConfig `yaml:"instances"`
	CheckHealth        *bool                   `yaml:"check-health"`
	HealthCheckTimeout durationField           `yaml:"health-check-timeout"`
	Timeout            durationField           `yaml:"timeout"`
	ShowServers        *bool                   `yaml:"show-servers"`
	ShowMetrics        *bool                   `yaml:"show-metrics"`
	ShowDatabases      *bool                   `yaml:"show-databases"`
	ShowFailingOnly    bool                    `yaml:"show-failing-only"`
	CollapseAfter      int                     `yaml:"collapse-after"`

	resolvedInstances []coolifyInstanceConfig `yaml:"-"`
	Configured        bool                    `yaml:"-"`
	HasToken          bool                    `yaml:"-"`
	APIHealthy        bool                    `yaml:"-"`
	HealthyInstances  int                     `yaml:"-"`
	TotalInstances    int                     `yaml:"-"`
	APILatencyMs      int64                   `yaml:"-"`
	Version           string                  `yaml:"-"`
	Servers           []coolifyServerItem     `yaml:"-"`
	Services          []coolifyServiceItem    `yaml:"-"`
	OnlineServers     int                     `yaml:"-"`
	HealthyServices   int                     `yaml:"-"`
	TotalServices     int                     `yaml:"-"`
	RunningDatabases  int                     `yaml:"-"`
	TotalDatabases    int                     `yaml:"-"`
}

type coolifyServerItem struct {
	UUID          string
	Instance      string
	Name          string
	IP            string
	Reachable     bool
	Usable        bool
	StateStyle    string
	StateLabel    string
	HasMetrics    bool
	HasCPU        bool
	CPUPercent    int
	CPUHigh       bool
	HasMemory     bool
	MemoryPercent int
	MemoryHigh    bool
	HasDisk       bool
	DiskPercent   int
	DiskHigh      bool
}

type coolifyServiceItem struct {
	Kind         string
	Name         string
	Subtitle     string
	URL          string
	CheckURL     string
	CoolifyState string
	HTTPText     string
	HTTPOK       bool
	HasHTTPCheck bool
	StateStyle   string
}

type coolifyServerJSON struct {
	UUID     string `json:"uuid"`
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Settings struct {
		IsReachable bool `json:"is_reachable"`
		IsUsable    bool `json:"is_usable"`
	} `json:"settings"`
}

type coolifyServerSentinelJSON struct {
	IsSentinelEnabled bool   `json:"is_sentinel_enabled"`
	IsMetricsEnabled  bool   `json:"is_metrics_enabled"`
	SentinelToken     string `json:"sentinel_token"`
	SentinelCustomURL string `json:"sentinel_custom_url"`
}

type coolifySentinelPercentPoint struct {
	Time    string  `json:"time"`
	Percent float64 `json:"percent"`
}

type coolifySentinelDiskPoint struct {
	Mount       string  `json:"mount"`
	UsedPercent float64 `json:"usedPercent"`
}

type coolifyApplicationJSON struct {
	UUID               string `json:"uuid"`
	Name               string `json:"name"`
	FQDN               string `json:"fqdn"`
	Status             string `json:"status"`
	HealthCheckEnabled bool   `json:"health_check_enabled"`
	HealthCheckPath    string `json:"health_check_path"`
	GitRepository      string `json:"git_repository"`
	GitBranch          string `json:"git_branch"`
	Destination        struct {
		Server struct {
			Name string `json:"name"`
		} `json:"server"`
	} `json:"destination"`
}

type coolifyServiceJSON struct {
	UUID   string `json:"uuid"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Server struct {
		Name string `json:"name"`
	} `json:"server"`
	Applications []struct {
		UUID   string `json:"uuid"`
		Name   string `json:"name"`
		FQDN   string `json:"fqdn"`
		Status string `json:"status"`
	} `json:"applications"`
	Databases []struct {
		UUID   string `json:"uuid"`
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"databases"`
}

type coolifyDatabaseJSON struct {
	UUID        string `json:"uuid"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	IsPublic    bool   `json:"is_public"`
	Destination struct {
		Server struct {
			Name string `json:"name"`
		} `json:"server"`
	} `json:"destination"`
}

type coolifyInstanceResult struct {
	instanceName string
	hasToken     bool
	healthy      bool
	latencyMs    int64
	version      string
	servers      []coolifyServerItem
	onlineSrv    int
	services     []coolifyServiceItem
	partialErr   error
	fatalErr     error
}

func (widget *coolifyWidget) initialize() error {
	widget.withTitle("Coolify & Services").withCacheDuration(1 * time.Minute)

	var instances []coolifyInstanceConfig
	if len(widget.Instances) > 0 {
		for _, inst := range widget.Instances {
			instURL := strings.TrimRight(strings.TrimSpace(inst.URL), "/")
			if instURL == "" {
				continue
			}
			instances = append(instances, coolifyInstanceConfig{
				Name:          strings.TrimSpace(inst.Name),
				URL:           instURL,
				Token:         strings.TrimSpace(inst.Token),
				SentinelURL:   strings.TrimRight(strings.TrimSpace(inst.SentinelURL), "/"),
				SentinelToken: strings.TrimSpace(inst.SentinelToken),
				AllowInsecure: inst.AllowInsecure || widget.AllowInsecure,
			})
		}
	} else {
		singleURL := strings.TrimRight(strings.TrimSpace(firstNonEmptyString(widget.URL, os.Getenv("COOLIFY_URL"))), "/")
		singleToken := strings.TrimSpace(firstNonEmptyString(widget.Token, os.Getenv("COOLIFY_API_TOKEN")))
		singleSentinelURL := strings.TrimRight(strings.TrimSpace(firstNonEmptyString(widget.SentinelURL, os.Getenv("COOLIFY_SENTINEL_URL"))), "/")
		singleSentinelToken := strings.TrimSpace(firstNonEmptyString(widget.SentinelToken, os.Getenv("COOLIFY_SENTINEL_TOKEN")))
		if singleURL != "" {
			instances = append(instances, coolifyInstanceConfig{
				URL:           singleURL,
				Token:         singleToken,
				SentinelURL:   singleSentinelURL,
				SentinelToken: singleSentinelToken,
				AllowInsecure: widget.AllowInsecure,
			})
		}
	}

	widget.resolvedInstances = instances

	if len(instances) == 1 && instances[0].URL != "" {
		widget.withTitleURL(instances[0].URL)
	}

	if widget.CheckHealth == nil {
		defaultTrue := true
		widget.CheckHealth = &defaultTrue
	}
	if widget.ShowServers == nil {
		defaultTrue := true
		widget.ShowServers = &defaultTrue
	}
	if widget.ShowMetrics == nil {
		defaultTrue := true
		widget.ShowMetrics = &defaultTrue
	}
	if widget.ShowDatabases == nil {
		defaultTrue := true
		widget.ShowDatabases = &defaultTrue
	}
	if widget.Timeout <= 0 {
		widget.Timeout = durationField(5 * time.Second)
	}
	if widget.HealthCheckTimeout <= 0 {
		widget.HealthCheckTimeout = durationField(3 * time.Second)
	}
	if widget.CollapseAfter == 0 || widget.CollapseAfter < -1 {
		widget.CollapseAfter = 8
	}

	return nil
}

func (widget *coolifyWidget) httpClient(allowInsecure bool) *http.Client {
	base := ternary(allowInsecure, defaultInsecureHTTPClient, defaultHTTPClient)
	return &http.Client{
		Transport: base.Transport,
		Timeout:   time.Duration(widget.Timeout),
	}
}

func (widget *coolifyWidget) update(_ context.Context) {
	if len(widget.resolvedInstances) == 0 {
		widget.Configured = false
		widget.HasToken = false
		widget.APIHealthy = false
		widget.HealthyInstances = 0
		widget.TotalInstances = 0
		widget.Servers = nil
		widget.Services = nil
		widget.withNotice(nil)
		widget.withError(nil).scheduleNextUpdate()
		return
	}

	widget.Configured = true
	widget.TotalInstances = len(widget.resolvedInstances)
	multiInstance := len(widget.resolvedInstances) > 1

	results := make([]coolifyInstanceResult, len(widget.resolvedInstances))
	var wg sync.WaitGroup
	for i, inst := range widget.resolvedInstances {
		wg.Add(1)
		go func(idx int, cfg coolifyInstanceConfig) {
			defer wg.Done()
			results[idx] = widget.fetchInstance(cfg, multiInstance)
		}(i, inst)
	}
	wg.Wait()

	var (
		healthyInstances int
		anyToken         bool
		maxLatencyMs     int64
		version          string
		allServers       []coolifyServerItem
		onlineServers    int
		allItems         []coolifyServiceItem
		firstFatalErr    error
		firstPartialErr  error
	)

	for i := range results {
		res := &results[i]
		if res.hasToken {
			anyToken = true
		}
		if res.latencyMs > maxLatencyMs {
			maxLatencyMs = res.latencyMs
		}
		if res.fatalErr != nil {
			if firstFatalErr == nil {
				firstFatalErr = res.fatalErr
			}
			continue
		}
		if res.healthy {
			healthyInstances++
		}
		if !multiInstance && res.version != "" {
			version = res.version
		}
		if res.partialErr != nil && firstPartialErr == nil {
			firstPartialErr = res.partialErr
		}
		allServers = append(allServers, res.servers...)
		onlineServers += res.onlineSrv
		allItems = append(allItems, res.services...)
	}

	if healthyInstances == 0 && firstFatalErr != nil {
		widget.APIHealthy = false
		widget.HealthyInstances = 0
		widget.canContinueUpdateAfterHandlingErr(firstFatalErr)
		return
	}

	if *widget.CheckHealth {
		runCoolifyHTTPHealthChecks(allItems, widget.HealthCheckTimeout, widget.AllowInsecure)
	}

	var healthySvcs, totalSvcs, runningDBs, totalDBs int
	for i := range allItems {
		finalizeCoolifyServiceItem(&allItems[i])
		if allItems[i].Kind == "DB" {
			totalDBs++
			if allItems[i].StateStyle == "ok" {
				runningDBs++
			}
		} else {
			totalSvcs++
			if allItems[i].StateStyle == "ok" {
				healthySvcs++
			}
		}
	}

	sortCoolifyServerItems(allServers)
	sortCoolifyServiceItems(allItems)

	if widget.ShowFailingOnly {
		filtered := make([]coolifyServiceItem, 0, len(allItems))
		for i := range allItems {
			if allItems[i].StateStyle != "ok" {
				filtered = append(filtered, allItems[i])
			}
		}
		allItems = filtered
	}

	widget.HasToken = anyToken
	widget.HealthyInstances = healthyInstances
	widget.APIHealthy = healthyInstances == len(widget.resolvedInstances)
	widget.APILatencyMs = maxLatencyMs
	widget.Version = version
	widget.Servers = allServers
	widget.OnlineServers = onlineServers
	widget.Services = allItems
	widget.HealthyServices = healthySvcs
	widget.TotalServices = totalSvcs
	widget.RunningDatabases = runningDBs
	widget.TotalDatabases = totalDBs

	if firstFatalErr != nil {
		widget.canContinueUpdateAfterHandlingErr(fmt.Errorf("%w: %v", errPartialContent, firstFatalErr))
		return
	}
	if firstPartialErr != nil {
		widget.canContinueUpdateAfterHandlingErr(fmt.Errorf("%w: %v", errPartialContent, firstPartialErr))
		return
	}

	widget.withNotice(nil)
	widget.withError(nil).scheduleNextUpdate()
}

func (widget *coolifyWidget) fetchInstance(cfg coolifyInstanceConfig, multiInstance bool) coolifyInstanceResult {
	client := widget.httpClient(cfg.AllowInsecure)
	res := coolifyInstanceResult{
		instanceName: cfg.Name,
		hasToken:     cfg.Token != "",
	}

	healthy, latencyMs, err := fetchCoolifyHealth(client, cfg.URL)
	res.latencyMs = latencyMs
	if err != nil {
		res.fatalErr = fmt.Errorf("checking Coolify health (%s): %w", firstNonEmptyString(cfg.Name, cfg.URL), err)
		return res
	}
	res.healthy = healthy

	if !res.hasToken {
		return res
	}

	var (
		wg          sync.WaitGroup
		version     string
		serversRaw  []coolifyServerJSON
		appsRaw     []coolifyApplicationJSON
		servicesRaw []coolifyServiceJSON
		dbsRaw      []coolifyDatabaseJSON
		errsMu      sync.Mutex
		fetchErrs   []error
	)

	recordErr := func(err error) {
		if err == nil {
			return
		}
		errsMu.Lock()
		fetchErrs = append(fetchErrs, err)
		errsMu.Unlock()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if v, err := fetchCoolifyText(client, cfg.URL+"/api/v1/version", cfg.Token); err == nil {
			version = strings.Trim(v, "\" \t\r\n")
		}
	}()

	if *widget.ShowServers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := fetchCoolifyJSON[[]coolifyServerJSON](client, cfg.URL+"/api/v1/servers", cfg.Token)
			if err != nil {
				recordErr(fmt.Errorf("servers: %w", err))
				return
			}
			serversRaw = s
		}()
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		apps, err := fetchCoolifyJSON[[]coolifyApplicationJSON](client, cfg.URL+"/api/v1/applications", cfg.Token)
		if err != nil {
			recordErr(fmt.Errorf("applications: %w", err))
			return
		}
		appsRaw = apps
	}()
	go func() {
		defer wg.Done()
		svcs, err := fetchCoolifyJSON[[]coolifyServiceJSON](client, cfg.URL+"/api/v1/services", cfg.Token)
		if err != nil {
			recordErr(fmt.Errorf("services: %w", err))
			return
		}
		servicesRaw = svcs
	}()

	if *widget.ShowDatabases {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dbs, err := fetchCoolifyJSON[[]coolifyDatabaseJSON](client, cfg.URL+"/api/v1/databases", cfg.Token)
			if err != nil {
				recordErr(fmt.Errorf("databases: %w", err))
				return
			}
			dbsRaw = dbs
		}()
	}

	wg.Wait()

	if len(fetchErrs) > 0 && len(appsRaw) == 0 && len(servicesRaw) == 0 && len(serversRaw) == 0 && len(dbsRaw) == 0 {
		res.fatalErr = fetchErrs[0]
		return res
	}

	res.version = version
	res.servers, res.onlineSrv = buildCoolifyServerItems(serversRaw, cfg.Name)

	if *widget.ShowServers && *widget.ShowMetrics && len(res.servers) > 0 {
		populateCoolifySentinelMetrics(client, cfg, res.servers)
	}

	scopeLabel := ""
	if multiInstance {
		scopeLabel = firstNonEmptyString(cfg.Name, strings.TrimPrefix(strings.TrimPrefix(cfg.URL, "https://"), "http://"))
	}
	res.services = buildCoolifyServiceItems(appsRaw, servicesRaw, dbsRaw, scopeLabel, len(res.servers) > 1)
	if len(fetchErrs) > 0 {
		res.partialErr = fetchErrs[0]
	}

	return res
}

func (widget *coolifyWidget) Render() template.HTML {
	return widget.renderTemplate(widget, coolifyWidgetTemplate)
}

func fetchCoolifyHealth(client *http.Client, baseURL string) (bool, int64, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/health", nil)
	if err != nil {
		return false, 0, err
	}

	start := time.Now()
	resp, err := client.Do(req)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return false, latencyMs, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if resp.StatusCode != http.StatusOK {
		return false, latencyMs, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return true, latencyMs, nil
}

func fetchCoolifyText(client *http.Client, endpoint, token string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/plain, application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	return strings.TrimSpace(string(body)), nil
}

func fetchCoolifyJSON[T any](client *http.Client, endpoint, token string) (T, error) {
	var zero T
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return zero, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	return decodeJsonFromRequest[T](client, req)
}

func buildCoolifyServerItems(servers []coolifyServerJSON, instanceName string) ([]coolifyServerItem, int) {
	items := make([]coolifyServerItem, 0, len(servers))
	online := 0

	for i := range servers {
		srv := &servers[i]
		isOnline := srv.Settings.IsReachable && srv.Settings.IsUsable
		stateStyle := "warn"
		stateLabel := "Unreachable"
		if isOnline {
			stateStyle = "ok"
			stateLabel = "Online"
			online++
		} else if srv.Settings.IsReachable {
			stateStyle = "muted"
			stateLabel = "Not usable"
		}

		items = append(items, coolifyServerItem{
			UUID:       srv.UUID,
			Instance:   instanceName,
			Name:       firstNonEmptyString(srv.Name, srv.UUID, "VPS"),
			IP:         srv.IP,
			Reachable:  srv.Settings.IsReachable,
			Usable:     srv.Settings.IsUsable,
			StateStyle: stateStyle,
			StateLabel: stateLabel,
		})
	}

	return items, online
}

func sortCoolifyServerItems(items []coolifyServerItem) {
	sort.Slice(items, func(a, b int) bool {
		return strings.ToLower(items[a].Name) < strings.ToLower(items[b].Name)
	})
}

func buildCoolifyServiceItems(
	apps []coolifyApplicationJSON,
	services []coolifyServiceJSON,
	databases []coolifyDatabaseJSON,
	instanceScope string,
	includeServerName bool,
) []coolifyServiceItem {
	items := make([]coolifyServiceItem, 0, len(apps)+len(services)+len(databases))

	appendScope := func(base, serverName string) string {
		var parts []string
		if instanceScope != "" {
			parts = append(parts, instanceScope)
		}
		if includeServerName && serverName != "" && !strings.EqualFold(serverName, instanceScope) {
			parts = append(parts, serverName)
		}
		if base != "" {
			parts = append(parts, base)
		}
		return strings.Join(parts, " · ")
	}

	for i := range apps {
		app := &apps[i]
		primaryURL := selectCoolifyPrimaryURL(app.FQDN)
		checkPath := ""
		if app.HealthCheckEnabled {
			checkPath = app.HealthCheckPath
		}
		checkURL := buildCoolifyCheckURL(primaryURL, checkPath)

		subtitle := strings.TrimSpace(app.GitRepository)
		if subtitle != "" && app.GitBranch != "" {
			subtitle += " (" + app.GitBranch + ")"
		}
		if subtitle == "" {
			subtitle = strings.TrimPrefix(strings.TrimPrefix(primaryURL, "https://"), "http://")
		}

		items = append(items, coolifyServiceItem{
			Kind:         "App",
			Name:         firstNonEmptyString(app.Name, app.UUID, "Application"),
			Subtitle:     appendScope(subtitle, app.Destination.Server.Name),
			URL:          primaryURL,
			CheckURL:     checkURL,
			CoolifyState: normalizeCoolifyStatus(app.Status),
		})
	}

	for i := range services {
		svc := &services[i]
		addedSubApp := false
		for j := range svc.Applications {
			subApp := &svc.Applications[j]
			primaryURL := selectCoolifyPrimaryURL(subApp.FQDN)
			if primaryURL == "" {
				continue
			}
			addedSubApp = true
			displayName := firstNonEmptyString(subApp.Name, svc.Name, svc.UUID)
			if svc.Name != "" && subApp.Name != "" && !strings.EqualFold(svc.Name, subApp.Name) {
				displayName = svc.Name + " · " + subApp.Name
			}
			host := strings.TrimPrefix(strings.TrimPrefix(primaryURL, "https://"), "http://")
			items = append(items, coolifyServiceItem{
				Kind:         "Service",
				Name:         displayName,
				Subtitle:     appendScope(host, svc.Server.Name),
				URL:          primaryURL,
				CheckURL:     buildCoolifyCheckURL(primaryURL, ""),
				CoolifyState: normalizeCoolifyStatus(firstNonEmptyString(subApp.Status, svc.Status)),
			})
		}

		if !addedSubApp {
			items = append(items, coolifyServiceItem{
				Kind:         "Service",
				Name:         firstNonEmptyString(svc.Name, svc.UUID, "Service"),
				Subtitle:     appendScope("Coolify stack", svc.Server.Name),
				CoolifyState: normalizeCoolifyStatus(svc.Status),
			})
		}
	}

	for i := range databases {
		db := &databases[i]
		dbType := strings.TrimPrefix(db.Type, "standalone-")
		items = append(items, coolifyServiceItem{
			Kind:         "DB",
			Name:         firstNonEmptyString(db.Name, db.UUID, "Database"),
			Subtitle:     appendScope(firstNonEmptyString(dbType, "database"), db.Destination.Server.Name),
			CoolifyState: normalizeCoolifyStatus(db.Status),
		})
	}

	return items
}

func runCoolifyHTTPHealthChecks(items []coolifyServiceItem, timeout durationField, allowInsecure bool) {
	var checkIndices []int
	var requests []*SiteStatusRequest

	for i := range items {
		if items[i].CheckURL == "" {
			continue
		}
		checkIndices = append(checkIndices, i)
		requests = append(requests, &SiteStatusRequest{
			DefaultURL:    items[i].URL,
			CheckURL:      items[i].CheckURL,
			AllowInsecure: allowInsecure,
			Timeout:       timeout,
		})
	}

	if len(requests) == 0 {
		return
	}

	statuses, err := fetchStatusForSites(requests)
	if err != nil {
		return
	}

	for idx, itemIdx := range checkIndices {
		if idx >= len(statuses) {
			break
		}
		status := &statuses[idx]
		item := &items[itemIdx]
		item.HasHTTPCheck = true

		if status.Error != nil {
			item.HTTPOK = false
			if status.TimedOut {
				item.HTTPText = "Timed Out"
			} else {
				item.HTTPText = "ERR"
			}
			continue
		}

		item.HTTPOK = status.Code >= 200 && status.Code < 400
		item.HTTPText = strconv.Itoa(status.Code) + " · " + strconv.FormatInt(status.ResponseTime.Milliseconds(), 10) + "ms"
	}
}

func finalizeCoolifyServiceItem(item *coolifyServiceItem) {
	stateLower := strings.ToLower(item.CoolifyState)

	coolifyBad := strings.Contains(stateLower, "unhealthy") ||
		strings.Contains(stateLower, "exited") ||
		strings.Contains(stateLower, "stopped") ||
		strings.Contains(stateLower, "dead") ||
		strings.Contains(stateLower, "error") ||
		strings.Contains(stateLower, "degraded")

	coolifyTransitioning := strings.Contains(stateLower, "starting") ||
		strings.Contains(stateLower, "restarting") ||
		strings.Contains(stateLower, "in_progress") ||
		strings.Contains(stateLower, "unknown")

	if coolifyBad || (item.HasHTTPCheck && !item.HTTPOK) {
		item.StateStyle = "warn"
	} else if coolifyTransitioning {
		item.StateStyle = "muted"
	} else {
		item.StateStyle = "ok"
	}
}

func sortCoolifyServiceItems(items []coolifyServiceItem) {
	priority := map[string]int{
		"warn":  0,
		"muted": 1,
		"ok":    2,
	}

	sort.SliceStable(items, func(a, b int) bool {
		if priority[items[a].StateStyle] != priority[items[b].StateStyle] {
			return priority[items[a].StateStyle] < priority[items[b].StateStyle]
		}
		return strings.ToLower(items[a].Name) < strings.ToLower(items[b].Name)
	})
}

func selectCoolifyPrimaryURL(fqdn string) string {
	if fqdn == "" {
		return ""
	}

	var fallback string
	for _, raw := range strings.Split(fqdn, ",") {
		candidate := strings.TrimSpace(raw)
		if candidate == "" {
			continue
		}
		if !strings.HasPrefix(candidate, "http://") && !strings.HasPrefix(candidate, "https://") {
			candidate = "https://" + candidate
		}
		if strings.HasPrefix(candidate, "https://") {
			return strings.TrimRight(candidate, "/")
		}
		if fallback == "" {
			fallback = strings.TrimRight(candidate, "/")
		}
	}

	return fallback
}

func buildCoolifyCheckURL(baseURL, healthPath string) string {
	if baseURL == "" {
		return ""
	}
	path := strings.TrimSpace(healthPath)
	if path == "" || path == "/" {
		return baseURL
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return baseURL + path
}

func normalizeCoolifyStatus(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "unknown"
	}
	return strings.ReplaceAll(trimmed, ":", " · ")
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func populateCoolifySentinelMetrics(coolifyClient *http.Client, cfg coolifyInstanceConfig, servers []coolifyServerItem) {
	sentinelClient := &http.Client{
		Transport: coolifyClient.Transport,
		Timeout:   2 * time.Second,
	}

	var wg sync.WaitGroup
	for i := range servers {
		srv := &servers[i]
		if !srv.Reachable || srv.UUID == "" {
			continue
		}

		wg.Add(1)
		go func(s *coolifyServerItem) {
			defer wg.Done()

			token := cfg.SentinelToken
			customURL := ""

			if token == "" || cfg.SentinelURL == "" {
				sentinelCfg, err := fetchCoolifyJSON[coolifyServerSentinelJSON](
					coolifyClient,
					cfg.URL+"/api/v1/servers/"+url.PathEscape(s.UUID)+"/sentinel",
					cfg.Token,
				)
				if err == nil {
					if token == "" {
						token = strings.TrimSpace(sentinelCfg.SentinelToken)
					}
					customURL = strings.TrimRight(strings.TrimSpace(sentinelCfg.SentinelCustomURL), "/")
					if cfg.SentinelURL == "" && cfg.SentinelToken == "" && (!sentinelCfg.IsSentinelEnabled || !sentinelCfg.IsMetricsEnabled) {
						return
					}
				}
			}

			if token == "" {
				return
			}

			candidates := buildSentinelCandidateURLs(cfg.SentinelURL, customURL, cfg.URL, s.IP)
			if len(candidates) == 0 {
				return
			}

			fromParam := url.QueryEscape(time.Now().Add(-15 * time.Minute).UTC().Format(time.RFC3339))
			var (
				workingBase string
				cpuPoints   []coolifySentinelPercentPoint
			)
			for _, candidate := range candidates {
				pts, err := fetchCoolifyJSON[[]coolifySentinelPercentPoint](
					sentinelClient,
					candidate+"/api/cpu/history?from="+fromParam,
					token,
				)
				if err == nil {
					workingBase = candidate
					cpuPoints = pts
					break
				}
			}

			if workingBase == "" {
				return
			}

			var (
				memPoints  []coolifySentinelPercentPoint
				memErr     error
				diskPoints []coolifySentinelDiskPoint
				diskErr    error
				mWg        sync.WaitGroup
			)
			mWg.Add(2)
			go func() {
				defer mWg.Done()
				memPoints, memErr = fetchCoolifyJSON[[]coolifySentinelPercentPoint](
					sentinelClient,
					workingBase+"/api/memory/history?from="+fromParam,
					token,
				)
			}()
			go func() {
				defer mWg.Done()
				diskPoints, diskErr = fetchCoolifyJSON[[]coolifySentinelDiskPoint](
					sentinelClient,
					workingBase+"/api/disk/history?from="+fromParam+"&mount=%2F",
					token,
				)
			}()
			mWg.Wait()

			if len(cpuPoints) > 0 {
				pct := int(math.Round(cpuPoints[len(cpuPoints)-1].Percent))
				s.HasCPU = true
				s.CPUPercent = max(0, pct)
				s.CPUHigh = s.CPUPercent >= 85
				s.HasMetrics = true
			}
			if memErr == nil && len(memPoints) > 0 {
				pct := int(math.Round(memPoints[len(memPoints)-1].Percent))
				s.HasMemory = true
				s.MemoryPercent = max(0, min(100, pct))
				s.MemoryHigh = s.MemoryPercent >= 85
				s.HasMetrics = true
			}
			if diskErr == nil && len(diskPoints) > 0 {
				pct := int(math.Round(diskPoints[len(diskPoints)-1].UsedPercent))
				s.HasDisk = true
				s.DiskPercent = max(0, min(100, pct))
				s.DiskHigh = s.DiskPercent >= 85
				s.HasMetrics = true
			}
		}(srv)
	}
	wg.Wait()
}

func buildSentinelCandidateURLs(explicitURL, customURL, coolifyURL, serverIP string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(raw string) {
		trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}

	add(explicitURL)
	add(customURL)

	ip := strings.TrimSpace(serverIP)
	isLocalhostIP := ip == "" || ip == "host.docker.internal" || ip == "127.0.0.1" || ip == "localhost"
	if !isLocalhostIP {
		add("http://" + ip + ":8888")
	} else {
		if parsed, err := url.Parse(coolifyURL); err == nil && parsed.Hostname() != "" {
			host := parsed.Hostname()
			if host != "127.0.0.1" && host != "localhost" && host != "host.docker.internal" {
				add("http://" + host + ":8888")
			}
		}
		add("http://coolify-sentinel:8888")
		add("http://host.docker.internal:8888")
		add("http://127.0.0.1:8888")
	}

	return out
}
