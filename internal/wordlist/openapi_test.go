package wordlist

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/RowanDark/kitestring/pkg/proute"
)

// routeMap indexes routes by "METHOD /path" for deterministic lookup.
func routeMap(routes []proute.Route) map[string]proute.Route {
	m := make(map[string]proute.Route, len(routes))
	for _, r := range routes {
		m[r.Method+" "+r.Path] = r
	}
	return m
}

// findCrumb returns the crumb with the given key, or zero value if absent.
func findCrumb(crumbs []proute.Crumb, key string) (proute.Crumb, bool) {
	for _, c := range crumbs {
		if c.Key == key {
			return c, true
		}
	}
	return proute.Crumb{}, false
}

// ---------- Swagger 2.0 fixture tests ----------

func TestParseSpec_Swagger2_RouteCount(t *testing.T) {
	data, err := os.ReadFile("testdata/swagger2.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	routes, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	// Expected: GET /v1/users, POST /v1/users, GET /v1/users/{id},
	//           DELETE /v1/users/{id}, GET /v1/items = 5 routes
	if len(routes) != 5 {
		t.Errorf("want 5 routes, got %d", len(routes))
		for _, r := range routes {
			t.Logf("  %s %s", r.Method, r.Path)
		}
	}
}

func TestParseSpec_Swagger2_MethodsPreserved(t *testing.T) {
	data, err := os.ReadFile("testdata/swagger2.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	routes, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	rm := routeMap(routes)

	for _, key := range []string{
		"GET /v1/users",
		"POST /v1/users",
		"GET /v1/users/{id}",
		"DELETE /v1/users/{id}",
		"GET /v1/items",
	} {
		if _, ok := rm[key]; !ok {
			t.Errorf("missing expected route %q", key)
		}
	}
}

func TestParseSpec_Swagger2_QueryParamTypes(t *testing.T) {
	data, err := os.ReadFile("testdata/swagger2.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	routes, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	rm := routeMap(routes)

	getUsers, ok := rm["GET /v1/users"]
	if !ok {
		t.Fatal("GET /v1/users not found")
	}

	cases := []struct {
		param    string
		wantType proute.CrumbType
	}{
		{"limit", proute.CrumbInt},
		{"email", proute.CrumbEmail},
		{"active", proute.CrumbBool},
	}
	for _, tc := range cases {
		c, ok := findCrumb(getUsers.QueryParams, tc.param)
		if !ok {
			t.Errorf("query param %q not found in GET /v1/users", tc.param)
			continue
		}
		if c.Type != tc.wantType {
			t.Errorf("param %q: want type %d, got %d", tc.param, tc.wantType, c.Type)
		}
	}
}

func TestParseSpec_Swagger2_HeaderExtracted(t *testing.T) {
	data, err := os.ReadFile("testdata/swagger2.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	routes, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	rm := routeMap(routes)

	postUsers, ok := rm["POST /v1/users"]
	if !ok {
		t.Fatal("POST /v1/users not found")
	}
	if _, ok := findCrumb(postUsers.Headers, "X-Request-ID"); !ok {
		t.Error("expected X-Request-ID header crumb in POST /v1/users")
	}
}

func TestParseSpec_Swagger2_BodyParamsExpanded(t *testing.T) {
	data, err := os.ReadFile("testdata/swagger2.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	routes, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	rm := routeMap(routes)

	postUsers, ok := rm["POST /v1/users"]
	if !ok {
		t.Fatal("POST /v1/users not found")
	}
	if len(postUsers.BodyParams) == 0 {
		t.Fatal("expected body params in POST /v1/users, got none")
	}

	emailCrumb, ok := findCrumb(postUsers.BodyParams, "email")
	if !ok {
		t.Fatal("body param 'email' not found")
	}
	if emailCrumb.Type != proute.CrumbEmail {
		t.Errorf("email param: want CrumbEmail (%d), got %d", proute.CrumbEmail, emailCrumb.Type)
	}
	if !emailCrumb.Required {
		t.Error("email param should be required")
	}

	activeCrumb, ok := findCrumb(postUsers.BodyParams, "active")
	if !ok {
		t.Fatal("body param 'active' not found")
	}
	if activeCrumb.Type != proute.CrumbBool {
		t.Errorf("active param: want CrumbBool (%d), got %d", proute.CrumbBool, activeCrumb.Type)
	}
}

func TestParseSpec_Swagger2_ContentType(t *testing.T) {
	data, err := os.ReadFile("testdata/swagger2.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	routes, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	rm := routeMap(routes)

	postUsers := rm["POST /v1/users"]
	if postUsers.ContentType != "application/json" {
		t.Errorf("want ContentType application/json, got %q", postUsers.ContentType)
	}
}

func TestParseSpec_Swagger2_BasePath(t *testing.T) {
	data, err := os.ReadFile("testdata/swagger2.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	routes, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	for _, r := range routes {
		if !strings.HasPrefix(r.Path, "/v1/") {
			t.Errorf("route %q missing /v1 basePath", r.Path)
		}
	}
}

// ---------- OpenAPI 3.0 fixture tests ----------

func TestParseSpec_OpenAPI3_RouteCount(t *testing.T) {
	data, err := os.ReadFile("testdata/openapi3.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	routes, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	// GET /items, POST /items, GET /orders/{orderId} = 3 routes
	if len(routes) != 3 {
		t.Errorf("want 3 routes, got %d", len(routes))
		for _, r := range routes {
			t.Logf("  %s %s", r.Method, r.Path)
		}
	}
}

func TestParseSpec_OpenAPI3_QueryParamTypes(t *testing.T) {
	data, err := os.ReadFile("testdata/openapi3.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	routes, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	rm := routeMap(routes)

	getItems, ok := rm["GET /items"]
	if !ok {
		t.Fatal("GET /items not found")
	}
	cases := []struct {
		param    string
		wantType proute.CrumbType
	}{
		{"page", proute.CrumbInt},
		{"active", proute.CrumbBool},
		{"tag", proute.CrumbString},
	}
	for _, tc := range cases {
		c, ok := findCrumb(getItems.QueryParams, tc.param)
		if !ok {
			t.Errorf("query param %q not found in GET /items", tc.param)
			continue
		}
		if c.Type != tc.wantType {
			t.Errorf("param %q: want type %d, got %d", tc.param, tc.wantType, c.Type)
		}
	}
}

func TestParseSpec_OpenAPI3_HeaderExtracted(t *testing.T) {
	data, err := os.ReadFile("testdata/openapi3.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	routes, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	rm := routeMap(routes)

	postItems, ok := rm["POST /items"]
	if !ok {
		t.Fatal("POST /items not found")
	}
	c, ok := findCrumb(postItems.Headers, "X-API-Key")
	if !ok {
		t.Fatal("expected X-API-Key header in POST /items")
	}
	if !c.Required {
		t.Error("X-API-Key should be required")
	}
}

func TestParseSpec_OpenAPI3_RequestBodyParams(t *testing.T) {
	data, err := os.ReadFile("testdata/openapi3.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	routes, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	rm := routeMap(routes)

	postItems, ok := rm["POST /items"]
	if !ok {
		t.Fatal("POST /items not found")
	}
	if len(postItems.BodyParams) == 0 {
		t.Fatal("expected body params in POST /items, got none")
	}

	cases := []struct {
		key      string
		wantType proute.CrumbType
	}{
		{"name", proute.CrumbString},
		{"price", proute.CrumbFloat},
		{"id", proute.CrumbUUID},
	}
	for _, tc := range cases {
		c, ok := findCrumb(postItems.BodyParams, tc.key)
		if !ok {
			t.Errorf("body param %q not found", tc.key)
			continue
		}
		if c.Type != tc.wantType {
			t.Errorf("param %q: want type %d, got %d", tc.key, tc.wantType, c.Type)
		}
	}

	nameCrumb, _ := findCrumb(postItems.BodyParams, "name")
	if !nameCrumb.Required {
		t.Error("body param 'name' should be required")
	}
}

func TestParseSpec_OpenAPI3_ContentType(t *testing.T) {
	data, err := os.ReadFile("testdata/openapi3.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	routes, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec: %v", err)
	}
	rm := routeMap(routes)

	postItems := rm["POST /items"]
	if postItems.ContentType != "application/json" {
		t.Errorf("want ContentType application/json, got %q", postItems.ContentType)
	}
}

// ---------- YAML format test ----------

func TestParseSpec_YAML_Swagger2(t *testing.T) {
	data, err := os.ReadFile("testdata/swagger2.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	routes, err := ParseSpec(data)
	if err != nil {
		t.Fatalf("ParseSpec YAML: %v", err)
	}
	// GET /api/ping, POST /api/echo = 2 routes
	if len(routes) != 2 {
		t.Errorf("want 2 routes, got %d", len(routes))
	}
	rm := routeMap(routes)
	if _, ok := rm["GET /api/ping"]; !ok {
		t.Error("missing GET /api/ping")
	}
	if _, ok := rm["POST /api/echo"]; !ok {
		t.Error("missing POST /api/echo")
	}
}

// ---------- FetchFromURL mock server test ----------

func TestFetchFromURL_MockServer(t *testing.T) {
	data, err := os.ReadFile("testdata/swagger2.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
	defer srv.Close()

	// Override HTTP client to use the test server's transport (it already uses http://).
	old := OpenAPIHTTPClient
	OpenAPIHTTPClient = srv.Client()
	defer func() { OpenAPIHTTPClient = old }()

	routes, err := FetchFromURL(srv.URL + "/swagger.json")
	if err != nil {
		t.Fatalf("FetchFromURL: %v", err)
	}
	if len(routes) == 0 {
		t.Fatal("expected routes from mock server, got none")
	}
}

func TestFetchFromURL_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	old := OpenAPIHTTPClient
	OpenAPIHTTPClient = srv.Client()
	defer func() { OpenAPIHTTPClient = old }()

	_, err := FetchFromURL(srv.URL + "/missing.json")
	if err == nil {
		t.Fatal("expected error for HTTP 404, got nil")
	}
}

// ---------- ListAPIsGuru mock test ----------

func TestListAPIsGuru_MockServer(t *testing.T) {
	catalogue := map[string]apisGuruAPI{
		"stripe.com": {
			Preferred: "v3",
			Versions: map[string]apisGuruVersion{
				"v3": {
					Info:       apisGuruInfo{Title: "Stripe API", Version: "2022-11-15"},
					OpenAPIURL: "https://example.com/stripe/openapi.json",
				},
			},
		},
		"github.com": {
			Preferred: "1.1.4",
			Versions: map[string]apisGuruVersion{
				"1.1.4": {
					Info:       apisGuruInfo{Title: "GitHub v3 REST API", Version: "1.1.4"},
					SwaggerURL: "https://example.com/github/swagger.json",
				},
			},
		},
	}
	data, _ := json.Marshal(catalogue)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
	defer srv.Close()

	old := OpenAPIHTTPClient
	oldURL := APIsGuruListURL
	OpenAPIHTTPClient = srv.Client()
	APIsGuruListURL = srv.URL + "/list.json"
	defer func() {
		OpenAPIHTTPClient = old
		APIsGuruListURL = oldURL
	}()

	entries, err := ListAPIsGuru("stripe")
	if err != nil {
		t.Fatalf("ListAPIsGuru: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].Name != "stripe.com" {
		t.Errorf("want stripe.com, got %q", entries[0].Name)
	}
	if entries[0].SpecURL != "https://example.com/stripe/openapi.json" {
		t.Errorf("unexpected SpecURL %q", entries[0].SpecURL)
	}

	// Empty filter returns all
	all, err := ListAPIsGuru("")
	if err != nil {
		t.Fatalf("ListAPIsGuru empty filter: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("want 2 entries with empty filter, got %d", len(all))
	}
}

// ---------- error handling ----------

func TestParseSpec_Invalid(t *testing.T) {
	_, err := ParseSpec([]byte(`not json or yaml at all }{`))
	if err == nil {
		t.Fatal("expected error for invalid input, got nil")
	}
}

func TestParseSpec_MissingVersionField(t *testing.T) {
	_, err := ParseSpec([]byte(`{"info": {"title": "no version"}}`))
	if err == nil {
		t.Fatal("expected error for missing swagger/openapi field, got nil")
	}
}

// ---------- type mapping unit tests ----------

func TestMapToCrumbType(t *testing.T) {
	cases := []struct {
		typ    string
		format string
		want   proute.CrumbType
	}{
		{"string", "", proute.CrumbString},
		{"string", "email", proute.CrumbEmail},
		{"string", "uuid", proute.CrumbUUID},
		{"integer", "", proute.CrumbInt},
		{"number", "", proute.CrumbFloat},
		{"boolean", "", proute.CrumbBool},
		{"", "", proute.CrumbString},
		{"object", "", proute.CrumbString},
	}
	for _, tc := range cases {
		got := mapToCrumbType(tc.typ, tc.format)
		if got != tc.want {
			t.Errorf("mapToCrumbType(%q, %q): want %d, got %d", tc.typ, tc.format, tc.want, got)
		}
	}
}
