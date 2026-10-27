package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/reconvault/reconvault-api/api"
	"github.com/reconvault/reconvault-api/internal/artifact"
	"github.com/reconvault/reconvault-api/internal/recon"
)

// openAPIDoc is the subset of api/swagger.json the contract tests read.
type openAPIDoc struct {
	OpenAPI    string                                `json:"openapi"`
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]openAPISchema `json:"schemas"`
	} `json:"components"`
}

type openAPISchema struct {
	Properties map[string]openAPISchema `json:"properties"`
	AllOf      []openAPISchema          `json:"allOf"`
}

func loadSpec(t *testing.T) openAPIDoc {
	t.Helper()
	var doc openAPIDoc
	if err := json.Unmarshal(api.Spec, &doc); err != nil {
		t.Fatalf("api/swagger.json is not valid JSON: %v", err)
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.0") {
		t.Fatalf("openapi = %q", doc.OpenAPI)
	}
	return doc
}

func TestOpenAPISpecMatchesRoutes(t *testing.T) {
	doc := loadSpec(t)
	var documented []string
	for path, item := range doc.Paths {
		for method := range item {
			if method != "parameters" {
				documented = append(documented, strings.ToUpper(method)+" "+path)
			}
		}
	}
	var registered []string
	for _, rt := range (&handlers{}).routes() {
		registered = append(registered, rt.method+" "+rt.path)
	}
	slices.Sort(documented)
	slices.Sort(registered)
	if !slices.Equal(documented, registered) {
		t.Errorf("swagger.json paths differ from registered routes:\n documented %v\n registered %v", documented, registered)
	}
}

// jsonFields returns the JSON property names of a struct type, skipping
// embedded structs (documented separately via allOf).
func jsonFields(t reflect.Type) []string {
	var out []string
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func keys(m map[string]openAPISchema) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestOpenAPISchemasMatchStructs(t *testing.T) {
	doc := loadSpec(t)
	schemas := doc.Components.Schemas
	tests := []struct {
		schema string
		props  map[string]openAPISchema
		typ    any
	}{
		{"ExecuteRequest", schemas["ExecuteRequest"].Properties, executeRequest{}},
		{"Artifact", schemas["Artifact"].Properties, artifact.Artifact{}},
		{"ArtifactSummary", schemas["ArtifactSummary"].Properties, artifact.Summary{}},
		{"SearchResult", schemas["SearchResult"].Properties, artifact.SearchResult{}},
		{"Domain", schemas["Domain"].Properties, artifact.Domain{}},
		{"ArtifactListMeta", schemas["ArtifactListMeta"].Properties, listMeta{}},
		{"SearchMeta", schemas["SearchMeta"].Properties, searchMeta{}},
		{"DomainMeta", schemas["DomainMeta"].Properties, domainMeta{}},
		{"DomainInfo", schemas["DomainInfo"].Properties, recon.Domain{}},
		{"DNSRecord", schemas["DNSRecord"].Properties, recon.DNSRecord{}},
		{"DNSRecordMeta", schemas["DNSRecordMeta"].Properties, dnsRecordsMeta{}},
		{"Subdomain", schemas["Subdomain"].Properties, recon.Subdomain{}},
		{"SubdomainMeta", schemas["SubdomainMeta"].Properties, subdomainsMeta{}},
		{"RootDomain", schemas["RootDomain"].Properties, recon.RootDomain{}},
		{"RootDomainMeta", schemas["RootDomainMeta"].Properties, rootDomainsMeta{}},
		{"Error.error", schemas["Error"].Properties["error"].Properties, errorDetail{}},
	}
	if allOf := schemas["ExecuteResponse"].AllOf; len(allOf) == 2 {
		tests = append(tests, struct {
			schema string
			props  map[string]openAPISchema
			typ    any
		}{"ExecuteResponse (extension)", allOf[1].Properties, executeResponse{}})
	} else {
		t.Error("ExecuteResponse must be allOf [Artifact, extension]")
	}
	for _, tt := range tests {
		got, want := keys(tt.props), jsonFields(reflect.TypeOf(tt.typ))
		if !slices.Equal(got, want) {
			t.Errorf("%s: schema properties %v, Go JSON fields %v", tt.schema, got, want)
		}
	}
}

func TestOpenAPISpecServed(t *testing.T) {
	h := NewHandler(&fakeService{}, fakePinger{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{APIKey: testKey, MaxRequestBytes: 1024})
	rec := do(t, h, "GET", "/swagger.json", "", func(r *http.Request) { r.Header.Del("Authorization") })
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status = %d content-type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !bytes.Equal(rec.Body.Bytes(), api.Spec) {
		t.Error("served document differs from the embedded spec")
	}
}
