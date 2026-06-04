package replay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RowanDark/kitestring/pkg/ksfile"
	"github.com/RowanDark/kitestring/pkg/proute"
)

// --- ParseResultLine tests ---

func TestParseResultLine_Pretty(t *testing.T) {
	// Pretty format after ANSI stripping: STATUS  METHOD  LENGTH  TIME  URL
	line := "\033[32m200\033[0m  POST    1337      42ms        https://target.com/api/v1/users"
	rr, err := ParseResultLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rr.Method != "POST" {
		t.Errorf("method: got %q, want %q", rr.Method, "POST")
	}
	if rr.URL != "https://target.com/api/v1/users" {
		t.Errorf("url: got %q, want %q", rr.URL, "https://target.com/api/v1/users")
	}
}

func TestParseResultLine_PrettyWithKSUID(t *testing.T) {
	line := "200  POST    1337      42ms        https://target.com/api/v1/users  a1b2c3d4e5f6"
	rr, err := ParseResultLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rr.KSUID != "a1b2c3d4e5f6" {
		t.Errorf("ksuid: got %q, want %q", rr.KSUID, "a1b2c3d4e5f6")
	}
}

func TestParseResultLine_Text(t *testing.T) {
	// Text format: METHOD  STATUS  LENGTH  TIME  URL
	line := "POST    200  1337      42ms        https://target.com/api/v1/users"
	rr, err := ParseResultLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rr.Method != "POST" {
		t.Errorf("method: got %q, want %q", rr.Method, "POST")
	}
	if rr.URL != "https://target.com/api/v1/users" {
		t.Errorf("url: got %q, want %q", rr.URL, "https://target.com/api/v1/users")
	}
}

func TestParseResultLine_TextWithKSUID(t *testing.T) {
	line := "GET     200  512       18ms        https://target.com/api/health  deadbeef0011"
	rr, err := ParseResultLine(line)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rr.KSUID != "deadbeef0011" {
		t.Errorf("ksuid: got %q, want %q", rr.KSUID, "deadbeef0011")
	}
	if rr.Method != "GET" {
		t.Errorf("method: got %q, want %q", rr.Method, "GET")
	}
}

func TestParseResultLine_JSONL(t *testing.T) {
	jr := struct {
		Method         string `json:"method"`
		URL            string `json:"url"`
		StatusCode     int    `json:"status_code"`
		ContentLength  int    `json:"content_length"`
		ResponseTimeMs int64  `json:"response_time_ms"`
		Timestamp      string `json:"timestamp"`
		KSUID          string `json:"ksuid"`
	}{
		Method:         "DELETE",
		URL:            "https://api.example.com/v2/items/42",
		StatusCode:     204,
		ContentLength:  0,
		ResponseTimeMs: 10,
		Timestamp:      "2025-06-03T12:00:00Z",
		KSUID:          "zz99test",
	}
	raw, _ := json.Marshal(jr)
	rr, err := ParseResultLine(string(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rr.Method != "DELETE" {
		t.Errorf("method: got %q, want %q", rr.Method, "DELETE")
	}
	if rr.URL != "https://api.example.com/v2/items/42" {
		t.Errorf("url: got %q, want %q", rr.URL, "https://api.example.com/v2/items/42")
	}
	if rr.KSUID != "zz99test" {
		t.Errorf("ksuid: got %q, want %q", rr.KSUID, "zz99test")
	}
}

func TestParseResultLine_TooFewFields(t *testing.T) {
	_, err := ParseResultLine("200 GET https://example.com")
	if err == nil {
		t.Fatal("expected error for too-few fields, got nil")
	}
}

func TestParseResultLine_Empty(t *testing.T) {
	_, err := ParseResultLine("")
	if err == nil {
		t.Fatal("expected error for empty line, got nil")
	}
}

// --- Reconstruct tests ---

// writeTestWordlist writes a .ks wordlist with a single route and returns the file path.
func writeTestWordlist(t *testing.T, route proute.Route) string {
	t.Helper()
	kf := ksfile.FromRoutes([]proute.Route{route}, ksfile.KSFileMeta{
		Name:   "test",
		Source: "test",
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ks")
	if err := ksfile.Write(path, kf); err != nil {
		t.Fatalf("write test wordlist: %v", err)
	}
	return path
}

func TestReconstruct_WithKSUID(t *testing.T) {
	route := proute.Route{
		Method:      "POST",
		Path:        "/api/v1/users",
		KSUID:       "testksuid123",
		ContentType: "application/json",
		BodyParams: []proute.Crumb{
			{Key: "name", Type: proute.CrumbString, Example: "alice"},
			{Key: "age", Type: proute.CrumbInt, Example: "30"},
		},
	}
	wlPath := writeTestWordlist(t, route)

	rr := &ReplayRequest{
		Method: "POST",
		URL:    "https://target.com/api/v1/users",
		KSUID:  "testksuid123",
	}

	req, err := rr.Reconstruct([]string{wlPath})
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}
	if req.Method != "POST" {
		t.Errorf("method: got %q, want POST", req.Method)
	}
	if req.URL.String() != "https://target.com/api/v1/users" {
		t.Errorf("url: got %q, want https://target.com/api/v1/users", req.URL.String())
	}
	// Body should be set (JSON with name + age)
	if req.Body == nil {
		t.Fatal("expected non-nil body for POST with body params")
	}
	ct := req.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}
}

func TestReconstruct_WithKSUID_QueryParams(t *testing.T) {
	route := proute.Route{
		Method: "GET",
		Path:   "/api/search",
		KSUID:  "searchksuid",
		QueryParams: []proute.Crumb{
			{Key: "q", Type: proute.CrumbString, Example: "hello"},
			{Key: "limit", Type: proute.CrumbInt, Example: "10"},
		},
	}
	wlPath := writeTestWordlist(t, route)

	rr := &ReplayRequest{
		Method: "GET",
		URL:    "https://target.com/api/search?q=old&limit=5",
		KSUID:  "searchksuid",
	}

	req, err := rr.Reconstruct([]string{wlPath})
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}
	q := req.URL.Query()
	if q.Get("q") != "hello" {
		t.Errorf("query param q: got %q, want %q", q.Get("q"), "hello")
	}
	if q.Get("limit") != "10" {
		t.Errorf("query param limit: got %q, want %q", q.Get("limit"), "10")
	}
}

func TestReconstruct_WithoutWordlist_FallsBackToURLMethod(t *testing.T) {
	rr := &ReplayRequest{
		Method: "GET",
		URL:    "https://target.com/api/health",
		KSUID:  "someid",
	}

	req, err := rr.Reconstruct(nil)
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}
	if req.Method != "GET" {
		t.Errorf("method: got %q, want GET", req.Method)
	}
	if req.URL.String() != "https://target.com/api/health" {
		t.Errorf("url: got %q", req.URL.String())
	}
}

func TestReconstruct_NoKSUID_FallsBackToURLMethod(t *testing.T) {
	rr := &ReplayRequest{
		Method: "DELETE",
		URL:    "https://target.com/api/v1/items/99",
	}

	req, err := rr.Reconstruct([]string{"/nonexistent/path.ks"})
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}
	if req.Method != "DELETE" {
		t.Errorf("method: got %q, want DELETE", req.Method)
	}
}

// --- Execute / proxy integration ---

func TestExecute_BasicRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	rr := &ReplayRequest{
		Method: "GET",
		URL:    srv.URL + "/ping",
	}

	resp, err := rr.Execute(srv.Client())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	if resp.Duration <= 0 {
		t.Error("duration should be positive")
	}
	if resp.RawRequest == "" {
		t.Error("RawRequest should be non-empty")
	}
	if !strings.Contains(resp.RawRequest, "GET /ping HTTP/1.1") {
		t.Errorf("RawRequest missing request line: %q", resp.RawRequest)
	}
}

func TestExecute_PostWithBody(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = buf[:n]
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	rr := &ReplayRequest{
		Method:  "POST",
		URL:     srv.URL + "/items",
		Body:    []byte(`{"name":"widget"}`),
		Headers: map[string]string{"Content-Type": "application/json"},
	}

	resp, err := rr.Execute(srv.Client())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status: got %d, want 201", resp.StatusCode)
	}
	if string(gotBody) != `{"name":"widget"}` {
		t.Errorf("server received body: %q", gotBody)
	}
}

// --- Proxy client tests ---

func TestNewProxyClient_HTTP(t *testing.T) {
	client, err := NewProxyClient("http://localhost:8080", false)
	if err != nil {
		t.Fatalf("NewProxyClient: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if transport.Proxy == nil {
		t.Error("expected Proxy to be set on transport")
	}
	// Verify proxy URL is correct
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://example.com", http.NoBody)
	proxyURL, _ := transport.Proxy(req)
	if proxyURL == nil {
		t.Fatal("proxy function returned nil URL")
	}
	if proxyURL.Host != "localhost:8080" {
		t.Errorf("proxy host: got %q, want localhost:8080", proxyURL.Host)
	}
}

func TestNewProxyClient_HTTPS(t *testing.T) {
	client, err := NewProxyClient("https://proxy.internal:8443", true)
	if err != nil {
		t.Fatalf("NewProxyClient: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if !transport.TLSClientConfig.InsecureSkipVerify {
		t.Error("expected InsecureSkipVerify=true")
	}
}

func TestNewProxyClient_SOCKS5(t *testing.T) {
	client, err := NewProxyClient("socks5://127.0.0.1:1080", false)
	if err != nil {
		t.Fatalf("NewProxyClient: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if transport.DialContext == nil {
		t.Error("expected DialContext to be set for SOCKS5 transport")
	}
}

func TestNewProxyClient_SOCKS5WithAuth(t *testing.T) {
	client, err := NewProxyClient("socks5://user:pass@127.0.0.1:1080", false)
	if err != nil {
		t.Fatalf("NewProxyClient: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewProxyClient_InvalidScheme(t *testing.T) {
	_, err := NewProxyClient("ftp://proxy.example.com:21", false)
	if err == nil {
		t.Fatal("expected error for unsupported scheme, got nil")
	}
}

func TestNewProxyClient_ThroughProxy(t *testing.T) {
	// Run a minimal HTTP proxy that records requests.
	var proxiedHost string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxiedHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	// Target server
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	client, err := NewProxyClient(proxy.URL, false)
	if err != nil {
		t.Fatalf("NewProxyClient: %v", err)
	}

	_, err = client.Get(target.URL + "/check")
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}

	targetParsed, _ := url.Parse(target.URL)
	if proxiedHost != targetParsed.Host {
		t.Errorf("proxy saw host %q, want %q", proxiedHost, targetParsed.Host)
	}
}

// --- formatRawRequest tests ---

func TestFormatRawRequest(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://example.com/api/v1/items", nil)
	req.Header.Set("Content-Type", "application/json")

	body := []byte(`{"key":"value"}`)
	raw := formatRawRequest(req, body)

	if !strings.Contains(raw, "POST /api/v1/items HTTP/1.1") {
		t.Errorf("missing request line in: %q", raw)
	}
	if !strings.Contains(raw, "Host: example.com") {
		t.Errorf("missing Host header in: %q", raw)
	}
	if !strings.Contains(raw, `{"key":"value"}`) {
		t.Errorf("missing body in: %q", raw)
	}
}

// --- OS stdin path helper ---

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
