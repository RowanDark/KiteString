package wordlist

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/RowanDark/kitestring/pkg/proute"
	"go.yaml.in/yaml/v3"
)

// OpenAPIHTTPClient is used for all OpenAPI fetch requests; override in tests.
var OpenAPIHTTPClient = &http.Client{Timeout: 30 * time.Second}

// APIsGuruListURL is the APIs.guru catalogue endpoint; override in tests.
var APIsGuruListURL = "https://api.apis.guru/v2/list.json"

// APIsGuruEntry describes a single entry from the APIs.guru catalogue.
type APIsGuruEntry struct {
	Name    string
	Title   string
	Version string
	SpecURL string
}

// FetchFromURL fetches a raw OpenAPI/Swagger JSON or YAML spec from any URL.
func FetchFromURL(specURL string) ([]proute.Route, error) {
	resp, err := OpenAPIHTTPClient.Get(specURL)
	if err != nil {
		return nil, fmt.Errorf("openapi: fetch %q: %w", specURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openapi: fetch %q: HTTP %d", specURL, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openapi: read body from %q: %w", specURL, err)
	}
	return ParseSpec(data)
}

// FetchFromFile reads a local OpenAPI/Swagger JSON or YAML spec file.
func FetchFromFile(path string) ([]proute.Route, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("openapi: read file %q: %w", path, err)
	}
	return ParseSpec(data)
}

// ParseSpec parses OpenAPI 2.0 (Swagger) and OpenAPI 3.x specs from raw bytes.
// Both JSON and YAML formats are supported.
func ParseSpec(data []byte) ([]proute.Route, error) {
	spec, err := decodeSpec(data)
	if err != nil {
		return nil, err
	}
	if sw, _ := strField(spec, "swagger"); strings.HasPrefix(sw, "2") {
		return parseSwagger2(spec)
	}
	if _, ok := strField(spec, "openapi"); ok {
		return parseOpenAPI3(spec)
	}
	return nil, fmt.Errorf("openapi: unrecognized spec format (missing swagger/openapi version field)")
}

// FetchFromAPIsGuru queries APIs.guru, finds the matching API by name, fetches
// its latest spec, and returns parsed routes.
func FetchFromAPIsGuru(apiName string) ([]proute.Route, error) {
	entries, err := ListAPIsGuru(apiName)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("openapi: no APIs.guru entry found matching %q", apiName)
	}
	return FetchFromURL(entries[0].SpecURL)
}

// ListAPIsGuru fetches the APIs.guru catalogue and returns entries matching
// filter (case-insensitive substring on API name). Empty filter returns all.
func ListAPIsGuru(filter string) ([]APIsGuruEntry, error) {
	resp, err := OpenAPIHTTPClient.Get(APIsGuruListURL)
	if err != nil {
		return nil, fmt.Errorf("openapi: fetch APIs.guru list: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openapi: fetch APIs.guru list: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openapi: read APIs.guru list: %w", err)
	}

	var raw map[string]apisGuruAPI
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("openapi: parse APIs.guru list: %w", err)
	}

	lf := strings.ToLower(filter)
	var entries []APIsGuruEntry
	for name, api := range raw {
		if lf != "" && !strings.Contains(strings.ToLower(name), lf) {
			continue
		}
		preferred := api.Preferred
		if preferred == "" {
			for k := range api.Versions {
				preferred = k
				break
			}
		}
		ver, ok := api.Versions[preferred]
		if !ok {
			continue
		}
		specURL := ver.OpenAPIURL
		if specURL == "" {
			specURL = ver.SwaggerURL
		}
		if specURL == "" {
			continue
		}
		entries = append(entries, APIsGuruEntry{
			Name:    name,
			Title:   ver.Info.Title,
			Version: ver.Info.Version,
			SpecURL: specURL,
		})
	}
	return entries, nil
}

type apisGuruAPI struct {
	Preferred string                     `json:"preferred"`
	Versions  map[string]apisGuruVersion `json:"versions"`
}

type apisGuruVersion struct {
	Info       apisGuruInfo `json:"info"`
	SwaggerURL string       `json:"swaggerUrl"`
	OpenAPIURL string       `json:"openApiUrl"`
}

type apisGuruInfo struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

// ---------- spec parsing ----------

var openAPIHTTPMethods = []string{"get", "post", "put", "delete", "patch", "options", "head", "trace"}

func decodeSpec(data []byte) (map[string]interface{}, error) {
	var spec map[string]interface{}
	if err := json.Unmarshal(data, &spec); err == nil {
		return spec, nil
	}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("openapi: not valid JSON or YAML: %w", err)
	}
	return spec, nil
}

func parseSwagger2(spec map[string]interface{}) ([]proute.Route, error) {
	basePath, _ := strField(spec, "basePath")
	if basePath == "/" {
		basePath = ""
	}

	paths, _ := mapField(spec, "paths")
	globalConsumes := strSliceField(spec, "consumes")

	var routes []proute.Route
	for pathStr, pathItem := range paths {
		piMap, ok := toMap(pathItem)
		if !ok {
			continue
		}
		pathLevelParams := paramSlice(piMap, "parameters")

		for _, method := range openAPIHTTPMethods {
			opItem, ok := piMap[method]
			if !ok {
				continue
			}
			opMap, ok := toMap(opItem)
			if !ok {
				continue
			}

			opParams := paramSlice(opMap, "parameters")
			allParams := mergeParams(pathLevelParams, opParams)

			r := proute.Route{
				Method: strings.ToUpper(method),
				Path:   basePath + pathStr,
				Source: "openapi",
			}

			// consumes → ContentType; operation-level overrides global
			if ct := strSliceField(opMap, "consumes"); len(ct) > 0 {
				r.ContentType = ct[0]
			} else if len(globalConsumes) > 0 {
				r.ContentType = globalConsumes[0]
			}

			for _, p := range allParams {
				pMap, ok := toMap(p)
				if !ok {
					continue
				}
				if ref, ok := strField(pMap, "$ref"); ok {
					pMap = resolveRef(spec, ref)
					if pMap == nil {
						continue
					}
				}
				in, _ := strField(pMap, "in")
				name, _ := strField(pMap, "name")
				required, _ := boolField(pMap, "required")
				typeStr, _ := strField(pMap, "type")
				formatStr, _ := strField(pMap, "format")
				crumbType := mapToCrumbType(typeStr, formatStr)
				example, _ := strField(pMap, "example")

				crumb := proute.Crumb{
					Key:      name,
					Type:     crumbType,
					Required: required,
					Example:  example,
				}

				switch in {
				case "query":
					r.QueryParams = append(r.QueryParams, crumb)
				case "header":
					r.Headers = append(r.Headers, crumb)
				case "body":
					schemaMap, _ := mapField(pMap, "schema")
					r.BodyParams = append(r.BodyParams, extractSchemaProps(spec, schemaMap)...)
				case "formData":
					r.BodyParams = append(r.BodyParams, crumb)
				}
			}
			routes = append(routes, r)
		}
	}
	return routes, nil
}

func parseOpenAPI3(spec map[string]interface{}) ([]proute.Route, error) {
	paths, _ := mapField(spec, "paths")

	var routes []proute.Route
	for pathStr, pathItem := range paths {
		piMap, ok := toMap(pathItem)
		if !ok {
			continue
		}
		pathLevelParams := paramSlice(piMap, "parameters")

		for _, method := range openAPIHTTPMethods {
			opItem, ok := piMap[method]
			if !ok {
				continue
			}
			opMap, ok := toMap(opItem)
			if !ok {
				continue
			}

			opParams := paramSlice(opMap, "parameters")
			allParams := mergeParams(pathLevelParams, opParams)

			r := proute.Route{
				Method: strings.ToUpper(method),
				Path:   pathStr,
				Source: "openapi",
			}

			for _, p := range allParams {
				pMap, ok := toMap(p)
				if !ok {
					continue
				}
				if ref, ok := strField(pMap, "$ref"); ok {
					pMap = resolveRef(spec, ref)
					if pMap == nil {
						continue
					}
				}
				in, _ := strField(pMap, "in")
				name, _ := strField(pMap, "name")
				required, _ := boolField(pMap, "required")
				schemaMap, _ := mapField(pMap, "schema")
				crumbType := schemaMapToCrumbType(schemaMap)
				example := firstExample(pMap, schemaMap)

				crumb := proute.Crumb{
					Key:      name,
					Type:     crumbType,
					Required: required,
					Example:  example,
				}
				switch in {
				case "query":
					r.QueryParams = append(r.QueryParams, crumb)
				case "header":
					r.Headers = append(r.Headers, crumb)
				}
			}

			// requestBody → BodyParams
			if rbItem, ok := opMap["requestBody"]; ok {
				rbMap, ok := toMap(rbItem)
				if ok {
					if ref, ok := strField(rbMap, "$ref"); ok {
						rbMap = resolveRef(spec, ref)
					}
					if rbMap != nil {
						contentMap, _ := mapField(rbMap, "content")
						for ct, ctItem := range contentMap {
							r.ContentType = ct
							ctMap, ok := toMap(ctItem)
							if !ok {
								break
							}
							schemaMap, _ := mapField(ctMap, "schema")
							if ref, ok := strField(schemaMap, "$ref"); ok {
								schemaMap = resolveRef(spec, ref)
							}
							r.BodyParams = extractSchemaProps(spec, schemaMap)
							break
						}
					}
				}
			}

			routes = append(routes, r)
		}
	}
	return routes, nil
}

// extractSchemaProps expands an object schema's properties into Crumbs.
func extractSchemaProps(spec, schema map[string]interface{}) []proute.Crumb {
	if schema == nil {
		return nil
	}
	if ref, ok := strField(schema, "$ref"); ok {
		schema = resolveRef(spec, ref)
		if schema == nil {
			return nil
		}
	}
	propsItem, ok := schema["properties"]
	if !ok {
		return nil
	}
	propsMap, ok := toMap(propsItem)
	if !ok {
		return nil
	}

	requiredSet := map[string]bool{}
	if reqSlice, ok := schema["required"]; ok {
		if arr, ok := reqSlice.([]interface{}); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					requiredSet[s] = true
				}
			}
		}
	}

	var crumbs []proute.Crumb
	for propName, propItem := range propsMap {
		propMap, ok := toMap(propItem)
		if !ok {
			continue
		}
		ex, _ := strField(propMap, "example")
		crumbs = append(crumbs, proute.Crumb{
			Key:      propName,
			Type:     schemaMapToCrumbType(propMap),
			Required: requiredSet[propName],
			Example:  ex,
		})
	}
	return crumbs
}

// resolveRef follows a same-document JSON Reference (e.g. "#/definitions/Foo").
func resolveRef(spec map[string]interface{}, ref string) map[string]interface{} {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	var cur interface{} = (interface{})(spec)
	for _, part := range parts {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = m[part]
	}
	m, _ := cur.(map[string]interface{})
	return m
}

// ---------- type mapping ----------

func mapToCrumbType(typ, format string) proute.CrumbType {
	switch typ {
	case "integer":
		return proute.CrumbInt
	case "number":
		return proute.CrumbFloat
	case "boolean":
		return proute.CrumbBool
	case "string":
		switch format {
		case "email":
			return proute.CrumbEmail
		case "uuid":
			return proute.CrumbUUID
		}
		return proute.CrumbString
	}
	return proute.CrumbString
}

func schemaMapToCrumbType(schema map[string]interface{}) proute.CrumbType {
	if schema == nil {
		return proute.CrumbString
	}
	typ, _ := strField(schema, "type")
	format, _ := strField(schema, "format")
	return mapToCrumbType(typ, format)
}

func firstExample(pMap, schemaMap map[string]interface{}) string {
	if ex, ok := strField(pMap, "example"); ok {
		return ex
	}
	if schemaMap != nil {
		if ex, ok := strField(schemaMap, "example"); ok {
			return ex
		}
	}
	return ""
}

// ---------- parameter merging ----------

// mergeParams merges path-level params with operation-level params, with
// operation-level taking precedence for the same name+in combination.
func mergeParams(pathParams, opParams []interface{}) []interface{} {
	if len(pathParams) == 0 {
		return opParams
	}
	if len(opParams) == 0 {
		return pathParams
	}
	type paramKey struct{ name, in string }
	overridden := map[paramKey]bool{}
	for _, p := range opParams {
		if m, ok := toMap(p); ok {
			n, _ := strField(m, "name")
			i, _ := strField(m, "in")
			overridden[paramKey{n, i}] = true
		}
	}
	var merged []interface{}
	for _, p := range pathParams {
		if m, ok := toMap(p); ok {
			n, _ := strField(m, "name")
			i, _ := strField(m, "in")
			if !overridden[paramKey{n, i}] {
				merged = append(merged, p)
			}
		}
	}
	return append(merged, opParams...)
}

// ---------- map helpers ----------

func paramSlice(m map[string]interface{}, key string) []interface{} {
	v, ok := m[key]
	if !ok {
		return nil
	}
	s, _ := v.([]interface{})
	return s
}

func strSliceField(m map[string]interface{}, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func strField(m map[string]interface{}, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func mapField(m map[string]interface{}, key string) (map[string]interface{}, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	return toMap(v)
}

func boolField(m map[string]interface{}, key string) (bool, bool) {
	v, ok := m[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func toMap(v interface{}) (map[string]interface{}, bool) {
	if v == nil {
		return nil, false
	}
	m, ok := v.(map[string]interface{})
	return m, ok
}
