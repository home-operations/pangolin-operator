package pangolin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Credentials holds the connection details parsed from a Kubernetes Secret
// (keys: PANGOLIN_ENDPOINT, PANGOLIN_API_KEY, PANGOLIN_ORG_ID).
type Credentials struct {
	Endpoint string
	APIKey   string
	OrgID    string
}

// ErrNotFound is returned by API calls when the Pangolin server responds with HTTP 404.
type ErrNotFound struct {
	Message string
}

func (e *ErrNotFound) Error() string { return e.Message }

// IsNotFound reports whether err (or any error it wraps) is an ErrNotFound.
func IsNotFound(err error) bool {
	var e *ErrNotFound
	return errors.As(err, &e)
}

type ErrConflict struct {
	Message string
}

func (e *ErrConflict) Error() string { return e.Message }

func IsConflict(err error) bool {
	var e *ErrConflict
	return errors.As(err, &e)
}

type API interface {
	// Sites
	PickSiteDefaults(ctx context.Context) (*PickSiteDefaultsResponse, error)
	CreateSite(ctx context.Context, req CreateSiteRequest) (*CreateSiteResponse, error)
	GetSite(ctx context.Context, siteID int) (*GetSiteResponse, error)
	UpdateSite(ctx context.Context, siteID int, req UpdateSiteRequest) error
	DeleteSite(ctx context.Context, siteID int) error

	// Domains
	ListDomains(ctx context.Context) ([]Domain, error)

	// Public resources
	CreateResource(ctx context.Context, req CreateResourceRequest) (*CreateResourceResponse, error)
	GetResource(ctx context.Context, resourceID int) (*GetResourceResponse, error)
	UpdateResource(ctx context.Context, resourceID int, req UpdateResourceRequest) error
	DeleteResource(ctx context.Context, resourceID int) error

	// Targets
	CreateTarget(ctx context.Context, resourceID int, req CreateTargetRequest) (*CreateTargetResponse, error)
	DeleteTarget(ctx context.Context, targetID int) error

	// Rules
	CreateRule(ctx context.Context, resourceID int, req CreateRuleRequest) (*CreateRuleResponse, error)
	DeleteRule(ctx context.Context, ruleID int) error

	// Private (VPN) resources
	CreateSiteResource(ctx context.Context, req CreateSiteResourceRequest) (*CreateSiteResourceResponse, error)
	GetSiteResource(ctx context.Context, siteResourceID int) (*GetSiteResourceResponse, error)
	UpdateSiteResource(ctx context.Context, siteResourceID int, req UpdateSiteResourceRequest) error
	DeleteSiteResource(ctx context.Context, siteResourceID int) error
}

// Compile-time check that *Client implements API.
var _ API = (*Client)(nil)

type Client struct {
	httpClient *http.Client
	endpoint   string
	apiKey     string
	orgID      string
}

func NewClient(creds Credentials) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		endpoint:   strings.TrimRight(creds.Endpoint, "/"),
		apiKey:     creds.APIKey,
		orgID:      creds.OrgID,
	}
}

func (c *Client) Endpoint() string { return c.endpoint }

func (c *Client) apiBase() string {
	return c.endpoint + "/v1"
}

// do executes an HTTP request. body is JSON-encoded when non-nil.
// On success the "data" field of the response envelope is decoded into out.
func (c *Client) do(ctx context.Context, method, url string, body, out any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check for 404 before attempting to decode — the server may return an HTML page
	// for missing resources rather than a JSON envelope.
	if resp.StatusCode == http.StatusNotFound {
		return &ErrNotFound{Message: fmt.Sprintf("not found: %s %s (HTTP 404)", method, url)}
	}

	if resp.StatusCode == http.StatusConflict {
		return &ErrConflict{Message: fmt.Sprintf("conflict: %s %s (HTTP 409)", method, url)}
	}

	var envelope struct {
		Data    json.RawMessage `json:"data"`
		Success bool            `json:"success"`
		Error   bool            `json:"error"`
		Message string          `json:"message"`
		Status  int             `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode response from %s %s (HTTP %d): %w",
			method, url, resp.StatusCode, err)
	}
	if !envelope.Success {
		if resp.StatusCode == http.StatusNotFound {
			return &ErrNotFound{Message: fmt.Sprintf("pangolin API error (HTTP 404): %s", envelope.Message)}
		}
		return fmt.Errorf("pangolin API error (HTTP %d, status %d): %s",
			resp.StatusCode, envelope.Status, envelope.Message)
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("decode response data from %s %s: %w", method, url, err)
		}
	}
	return nil
}

type PickSiteDefaultsResponse struct {
	NewtID        string `json:"newtId"`
	NewtSecret    string `json:"newtSecret"`
	ClientAddress string `json:"clientAddress"`
}

func (c *Client) PickSiteDefaults(ctx context.Context) (*PickSiteDefaultsResponse, error) {
	url := fmt.Sprintf("%s/org/%s/pick-site-defaults", c.apiBase(), c.orgID)
	var out PickSiteDefaultsResponse
	if err := c.do(ctx, http.MethodGet, url, nil, &out); err != nil {
		return nil, fmt.Errorf("PickSiteDefaults: %w", err)
	}
	return &out, nil
}

type CreateSiteRequest struct {
	Name    string `json:"name"`
	Address string `json:"address"` // clientAddress from PickSiteDefaults
	Type    string `json:"type"`    // always "newt"
	NewtID  string `json:"newtId"`
	Secret  string `json:"secret"`
}

type CreateSiteResponse struct {
	SiteID int    `json:"siteId"`
	NiceID string `json:"niceId"`
}

func (c *Client) CreateSite(ctx context.Context, req CreateSiteRequest) (*CreateSiteResponse, error) {
	url := fmt.Sprintf("%s/org/%s/site", c.apiBase(), c.orgID)
	var out CreateSiteResponse
	if err := c.do(ctx, http.MethodPut, url, req, &out); err != nil {
		return nil, fmt.Errorf("CreateSite: %w", err)
	}
	return &out, nil
}

type GetSiteResponse struct {
	SiteID  int    `json:"siteId"`
	NiceID  string `json:"niceId"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Online  bool   `json:"online"`
	Address string `json:"address"`
}

func (c *Client) GetSite(ctx context.Context, siteID int) (*GetSiteResponse, error) {
	url := fmt.Sprintf("%s/site/%d", c.apiBase(), siteID)
	var out GetSiteResponse
	if err := c.do(ctx, http.MethodGet, url, nil, &out); err != nil {
		return nil, fmt.Errorf("GetSite(%d): %w", siteID, err)
	}
	return &out, nil
}

type UpdateSiteRequest struct {
	Name string `json:"name,omitempty"`
}

func (c *Client) UpdateSite(ctx context.Context, siteID int, req UpdateSiteRequest) error {
	url := fmt.Sprintf("%s/site/%d", c.apiBase(), siteID)
	if err := c.do(ctx, http.MethodPost, url, req, nil); err != nil {
		return fmt.Errorf("UpdateSite(%d): %w", siteID, err)
	}
	return nil
}

func (c *Client) DeleteSite(ctx context.Context, siteID int) error {
	url := fmt.Sprintf("%s/site/%d", c.apiBase(), siteID)
	if err := c.do(ctx, http.MethodDelete, url, nil, nil); err != nil {
		return fmt.Errorf("DeleteSite(%d): %w", siteID, err)
	}
	return nil
}

type Domain struct {
	DomainID   string `json:"domainId"`
	BaseDomain string `json:"baseDomain"`
}

type ListDomainsResponse struct {
	Domains []Domain `json:"domains"`
}

func (c *Client) ListDomains(ctx context.Context) ([]Domain, error) {
	url := fmt.Sprintf("%s/org/%s/domains", c.apiBase(), c.orgID)
	var out ListDomainsResponse
	if err := c.do(ctx, http.MethodGet, url, nil, &out); err != nil {
		return nil, fmt.Errorf("ListDomains: %w", err)
	}
	return out.Domains, nil
}

// ResolveDomainID returns the domainId whose baseDomain is a suffix of fullDomain.
// It prefers the longest matching suffix (most-specific domain wins).
func ResolveDomainID(domains []Domain, fullDomain string) (string, bool) {
	best := ""
	bestLen := 0
	for _, d := range domains {
		if d.BaseDomain == fullDomain || strings.HasSuffix(fullDomain, "."+d.BaseDomain) {
			if len(d.BaseDomain) > bestLen {
				best = d.DomainID
				bestLen = len(d.BaseDomain)
			}
		}
	}
	return best, best != ""
}

type CreateResourceRequest struct {
	Name     string `json:"name"`
	Http     bool   `json:"http"`
	Protocol string `json:"protocol"`
	// ProxyPort is required for tcp/udp resources; omitted for http resources.
	ProxyPort int    `json:"proxyPort,omitempty"`
	DomainId  string `json:"domainId,omitempty"`
	Subdomain string `json:"subdomain,omitempty"`
}

type CreateResourceResponse struct {
	ResourceID int    `json:"resourceId"`
	NiceID     string `json:"niceId"`
	FullDomain string `json:"fullDomain"`
}

func (c *Client) CreateResource(ctx context.Context, req CreateResourceRequest) (*CreateResourceResponse, error) {
	url := fmt.Sprintf("%s/org/%s/resource", c.apiBase(), c.orgID)
	var out CreateResourceResponse
	if err := c.do(ctx, http.MethodPut, url, req, &out); err != nil {
		return nil, fmt.Errorf("CreateResource: %w", err)
	}
	return &out, nil
}

type GetResourceResponse struct {
	ResourceID int    `json:"resourceId"`
	NiceID     string `json:"niceId"`
	Name       string `json:"name"`
	FullDomain string `json:"fullDomain"`
	Subdomain  string `json:"subdomain"`
	DomainID   string `json:"domainId"`
	Enabled    bool   `json:"enabled"`
}

func (c *Client) GetResource(ctx context.Context, resourceID int) (*GetResourceResponse, error) {
	url := fmt.Sprintf("%s/resource/%d", c.apiBase(), resourceID)
	var out GetResourceResponse
	if err := c.do(ctx, http.MethodGet, url, nil, &out); err != nil {
		return nil, fmt.Errorf("GetResource(%d): %w", resourceID, err)
	}
	return &out, nil
}

type UpdateResourceRequest struct {
	Name                  string  `json:"name,omitempty"`
	Subdomain             string  `json:"subdomain,omitempty"`
	Ssl                   *bool   `json:"ssl,omitempty"`
	Sso                   *bool   `json:"sso,omitempty"`
	BlockAccess           *bool   `json:"blockAccess,omitempty"`
	EmailWhitelistEnabled *bool   `json:"emailWhitelistEnabled,omitempty"`
	ApplyRules            *bool   `json:"applyRules,omitempty"`
	Enabled               bool    `json:"enabled,omitempty"`
	StickySession         *bool   `json:"stickySession,omitempty"`
	TlsServerName         *string `json:"tlsServerName,omitempty"`
	SetHostHeader         *string `json:"setHostHeader,omitempty"`
	SkipToIdpId           *int    `json:"skipToIdpId,omitempty"`
}

func (c *Client) UpdateResource(ctx context.Context, resourceID int, req UpdateResourceRequest) error {
	url := fmt.Sprintf("%s/resource/%d", c.apiBase(), resourceID)
	if err := c.do(ctx, http.MethodPost, url, req, nil); err != nil {
		return fmt.Errorf("UpdateResource(%d): %w", resourceID, err)
	}
	return nil
}

func (c *Client) DeleteResource(ctx context.Context, resourceID int) error {
	url := fmt.Sprintf("%s/resource/%d", c.apiBase(), resourceID)
	if err := c.do(ctx, http.MethodDelete, url, nil, nil); err != nil {
		return fmt.Errorf("DeleteResource(%d): %w", resourceID, err)
	}
	return nil
}

type CreateRuleRequest struct {
	Action   string `json:"action"` // "ACCEPT", "DROP", "PASS"
	Match    string `json:"match"`  // "CIDR", "IP", "PATH", "COUNTRY", "ASN"
	Value    string `json:"value"`
	Priority int    `json:"priority"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

type CreateRuleResponse struct {
	RuleID int `json:"ruleId"`
}

func (c *Client) CreateRule(ctx context.Context, resourceID int, req CreateRuleRequest) (*CreateRuleResponse, error) {
	url := fmt.Sprintf("%s/resource/%d/rule", c.apiBase(), resourceID)
	var out CreateRuleResponse
	if err := c.do(ctx, http.MethodPut, url, req, &out); err != nil {
		return nil, fmt.Errorf("CreateRule(resource=%d): %w", resourceID, err)
	}
	return &out, nil
}

func (c *Client) DeleteRule(ctx context.Context, ruleID int) error {
	url := fmt.Sprintf("%s/rule/%d", c.apiBase(), ruleID)
	if err := c.do(ctx, http.MethodDelete, url, nil, nil); err != nil {
		return fmt.Errorf("DeleteRule(%d): %w", ruleID, err)
	}
	return nil
}

type CreateTargetRequest struct {
	SiteID          int    `json:"siteId"`
	Ip              string `json:"ip"`
	Port            int    `json:"port"`
	Method          string `json:"method,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty"`
	Path            string `json:"path,omitempty"`
	PathMatchType   string `json:"pathMatchType,omitempty"`
	RewritePath     string `json:"rewritePath,omitempty"`
	RewritePathType string `json:"rewritePathType,omitempty"`
	Priority        int    `json:"priority,omitempty"`
}

type CreateTargetResponse struct {
	TargetID int `json:"targetId"`
}

func (c *Client) CreateTarget(ctx context.Context, resourceID int, req CreateTargetRequest) (*CreateTargetResponse, error) {
	url := fmt.Sprintf("%s/resource/%d/target", c.apiBase(), resourceID)
	var out CreateTargetResponse
	if err := c.do(ctx, http.MethodPut, url, req, &out); err != nil {
		return nil, fmt.Errorf("CreateTarget(resource=%d): %w", resourceID, err)
	}
	return &out, nil
}

func (c *Client) DeleteTarget(ctx context.Context, targetID int) error {
	url := fmt.Sprintf("%s/target/%d", c.apiBase(), targetID)
	if err := c.do(ctx, http.MethodDelete, url, nil, nil); err != nil {
		return fmt.Errorf("DeleteTarget(%d): %w", targetID, err)
	}
	return nil
}

type CreateSiteResourceRequest struct {
	Name               string   `json:"name"`
	SiteID             int      `json:"siteId"`
	Mode               string   `json:"mode"`        // "host", "cidr"
	Destination        string   `json:"destination"` // IP, hostname, or CIDR
	TcpPortRangeString string   `json:"tcpPortRangeString"`
	UdpPortRangeString string   `json:"udpPortRangeString"`
	DisableIcmp        bool     `json:"disableIcmp,omitempty"`
	Alias              string   `json:"alias,omitempty"`
	RoleIds            []int    `json:"roleIds"`
	UserIds            []string `json:"userIds"`
	ClientIds          []int    `json:"clientIds"`
}

type CreateSiteResourceResponse struct {
	SiteResourceID int    `json:"siteResourceId"`
	NiceID         string `json:"niceId,omitempty"`
}

func (c *Client) CreateSiteResource(ctx context.Context, req CreateSiteResourceRequest) (*CreateSiteResourceResponse, error) {
	url := fmt.Sprintf("%s/org/%s/site-resource", c.apiBase(), c.orgID)
	var out CreateSiteResourceResponse
	if err := c.do(ctx, http.MethodPut, url, req, &out); err != nil {
		return nil, fmt.Errorf("CreateSiteResource(site=%d): %w", req.SiteID, err)
	}
	return &out, nil
}

type GetSiteResourceResponse struct {
	SiteResourceID int    `json:"siteResourceId"`
	SiteID         int    `json:"siteId"`
	NiceID         string `json:"niceId"`
	Name           string `json:"name"`
	Mode           string `json:"mode"`
	Destination    string `json:"destination"`
	Enabled        bool   `json:"enabled"`
}

func (c *Client) GetSiteResource(ctx context.Context, siteResourceID int) (*GetSiteResourceResponse, error) {
	url := fmt.Sprintf("%s/site-resource/%d", c.apiBase(), siteResourceID)
	var out GetSiteResourceResponse
	if err := c.do(ctx, http.MethodGet, url, nil, &out); err != nil {
		return nil, fmt.Errorf("GetSiteResource(%d): %w", siteResourceID, err)
	}
	return &out, nil
}

type UpdateSiteResourceRequest struct {
	// siteId, userIds, roleIds, clientIds are required by the API even on update.
	SiteID             int      `json:"siteId"`
	UserIds            []string `json:"userIds"`
	RoleIds            []int    `json:"roleIds"`
	ClientIds          []int    `json:"clientIds"`
	Name               string   `json:"name,omitempty"`
	Mode               string   `json:"mode,omitempty"`
	Destination        string   `json:"destination,omitempty"`
	TcpPortRangeString string   `json:"tcpPortRangeString,omitempty"`
	UdpPortRangeString string   `json:"udpPortRangeString,omitempty"`
	DisableIcmp        bool     `json:"disableIcmp,omitempty"`
	Alias              string   `json:"alias,omitempty"`
}

func (c *Client) UpdateSiteResource(ctx context.Context, siteResourceID int, req UpdateSiteResourceRequest) error {
	url := fmt.Sprintf("%s/site-resource/%d", c.apiBase(), siteResourceID)
	if err := c.do(ctx, http.MethodPost, url, req, nil); err != nil {
		return fmt.Errorf("UpdateSiteResource(%d): %w", siteResourceID, err)
	}
	return nil
}

func (c *Client) DeleteSiteResource(ctx context.Context, siteResourceID int) error {
	url := fmt.Sprintf("%s/site-resource/%d", c.apiBase(), siteResourceID)
	if err := c.do(ctx, http.MethodDelete, url, nil, nil); err != nil {
		return fmt.Errorf("DeleteSiteResource(%d): %w", siteResourceID, err)
	}
	return nil
}
