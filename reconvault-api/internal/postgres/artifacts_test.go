package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reconvault/reconvault-api/internal/artifact"
	"github.com/reconvault/reconvault-api/internal/testdb"
)

var base = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

type seed struct {
	method, host, path string
	at                 time.Duration // offset from base
	status             *int
	ctype              *string
	size               *int64
	headers            string
	reqBody, resp      *string
}

func insert(t *testing.T, repo *ArtifactRepository, s seed) *artifact.Artifact {
	t.Helper()
	a := &artifact.Artifact{
		HTTPMethod: s.method, Hostname: s.host, EndpointPath: s.path,
		FullURL:     "https://" + s.host + s.path,
		RequestedAt: base.Add(s.at), ResponseStatusCode: s.status, ResponseContentType: s.ctype,
		ResponseBodySizeBytes: s.size, RequestBody: s.reqBody, Response: s.resp,
	}
	if s.headers != "" {
		a.RequestHeaders = json.RawMessage(s.headers)
	}
	if err := repo.Create(context.Background(), a); err != nil {
		t.Fatalf("create: %v", err)
	}
	return a
}

func setup(t *testing.T) (*ArtifactRepository, *pgxpool.Pool) {
	pool := testdb.New(t)
	return NewArtifactRepository(pool), pool
}

func TestCreateAndGetRoundTrip(t *testing.T) {
	repo, _ := setup(t)
	in := insert(t, repo, seed{
		method: "POST", host: "api.example.com", path: "/v1/resource", at: 1500 * time.Microsecond,
		status: ptr(200), ctype: ptr("application/json"), size: ptr(int64(16)),
		headers: `{"Content-Type":"application/json","User-Agent":"ua"}`,
		reqBody: ptr(`{"id":123,"name":"test"}`), resp: ptr(`{"success":true}`),
	})
	if !artifact.IsUUID(in.ID) {
		t.Fatalf("id = %q", in.ID)
	}
	got, err := repo.Get(context.Background(), in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != in.ID || got.HTTPMethod != "POST" || got.Hostname != "api.example.com" || got.EndpointPath != "/v1/resource" ||
		got.FullURL != "https://api.example.com/v1/resource" || !got.RequestedAt.Equal(in.RequestedAt) ||
		got.RequestedAt.Location() != time.UTC || *got.ResponseStatusCode != 200 || *got.ResponseContentType != "application/json" ||
		*got.ResponseBodySizeBytes != 16 || *got.RequestBody != *in.RequestBody || *got.Response != *in.Response {
		t.Errorf("round trip mismatch: %+v", got)
	}
	var h map[string]string
	if err := json.Unmarshal(got.RequestHeaders, &h); err != nil || h["User-Agent"] != "ua" {
		t.Errorf("headers = %s (%v)", got.RequestHeaders, err)
	}
}

func TestCreateAndGetNulls(t *testing.T) {
	repo, _ := setup(t)
	in := insert(t, repo, seed{method: "GET", host: "a.com", path: "/"})
	got, err := repo.Get(context.Background(), in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ResponseStatusCode != nil || got.ResponseContentType != nil || got.ResponseBodySizeBytes != nil ||
		got.RequestHeaders != nil || got.RequestBody != nil || got.Response != nil {
		t.Errorf("nullable fields must round-trip as NULL: %+v", got)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"request_headers":null`) {
		t.Errorf("JSON = %s", b)
	}
}

func TestCreateLargeAndUnicodeBodies(t *testing.T) {
	repo, _ := setup(t)
	big := strings.Repeat("日本語 secret ", 200_000) // ~3.4 MB
	in := insert(t, repo, seed{method: "GET", host: "a.com", path: "/", resp: &big})
	got, err := repo.Get(context.Background(), in.ID)
	if err != nil || *got.Response != big {
		t.Fatalf("large body round trip failed: %v", err)
	}
}

// Inserts must not fail on B-tree index entry limits for maximum-size input.
func TestCreateMaximumSizeFields(t *testing.T) {
	repo, _ := setup(t)
	host := strings.Repeat("h", artifact.MaxHostnameLength)
	path := "/" + strings.Repeat("p", artifact.MaxURLLength)
	ctype := strings.Repeat("c", 64<<10)
	insert(t, repo, seed{method: "OPTIONS", host: host, path: path, ctype: &ctype})
	got, err := repo.List(context.Background(), artifact.ListParams{SortBy: "full_url", SortOrder: "ASC", Hostname: host, Limit: 1})
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestGetNotFound(t *testing.T) {
	repo, _ := setup(t)
	_, err := repo.Get(context.Background(), "550e8400-e29b-41d4-a716-446655440000")
	if !errors.Is(err, artifact.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func ids[T any](items []T, id func(T) string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = id(it)
	}
	return out
}

func summaryID(s artifact.Summary) string { return s.ID }

func TestList(t *testing.T) {
	repo, _ := setup(t)
	a := insert(t, repo, seed{method: "GET", host: "a.com", path: "/1", at: 1 * time.Second, status: ptr(500), ctype: ptr("text/html"), size: ptr(int64(30))})
	b := insert(t, repo, seed{method: "POST", host: "b.com", path: "/2", at: 3 * time.Second, status: ptr(200), ctype: ptr("application/json"), size: ptr(int64(10))})
	c := insert(t, repo, seed{method: "DELETE", host: "a.com", path: "/3", at: 2 * time.Second, status: ptr(404), ctype: ptr("text/plain"), size: ptr(int64(20))})
	d := insert(t, repo, seed{method: "PUT", host: "c.com", path: "/4", at: 0}) // failed request: NULL response fields

	tests := []struct {
		name string
		p    artifact.ListParams
		want []string
	}{
		{"default requested_at desc", artifact.ListParams{SortBy: "requested_at", SortOrder: "DESC", Limit: 30}, []string{b.ID, c.ID, a.ID, d.ID}},
		{"requested_at asc", artifact.ListParams{SortBy: "requested_at", SortOrder: "ASC", Limit: 30}, []string{d.ID, a.ID, c.ID, b.ID}},
		{"limit", artifact.ListParams{SortBy: "requested_at", SortOrder: "DESC", Limit: 2}, []string{b.ID, c.ID}},
		{"hostname filter", artifact.ListParams{SortBy: "requested_at", SortOrder: "DESC", Hostname: "a.com", Limit: 30}, []string{c.ID, a.ID}},
		{"hostname no match", artifact.ListParams{SortBy: "requested_at", SortOrder: "DESC", Hostname: "zzz.com", Limit: 30}, []string{}},
		{"status asc nulls last", artifact.ListParams{SortBy: "response_status_code", SortOrder: "ASC", Limit: 30}, []string{b.ID, c.ID, a.ID, d.ID}},
		{"status desc nulls first", artifact.ListParams{SortBy: "response_status_code", SortOrder: "DESC", Limit: 30}, []string{d.ID, a.ID, c.ID, b.ID}},
		{"method asc", artifact.ListParams{SortBy: "http_method", SortOrder: "ASC", Limit: 30}, []string{c.ID, a.ID, b.ID, d.ID}},
		{"full_url desc", artifact.ListParams{SortBy: "full_url", SortOrder: "DESC", Limit: 30}, []string{d.ID, b.ID, c.ID, a.ID}},
		{"content type asc", artifact.ListParams{SortBy: "response_content_type", SortOrder: "ASC", Limit: 30}, []string{b.ID, a.ID, c.ID, d.ID}},
		{"size asc", artifact.ListParams{SortBy: "response_body_size_bytes", SortOrder: "ASC", Limit: 30}, []string{b.ID, c.ID, a.ID, d.ID}},
		{"filter + sort", artifact.ListParams{SortBy: "response_body_size_bytes", SortOrder: "ASC", Hostname: "a.com", Limit: 1}, []string{c.ID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.List(context.Background(), tt.p)
			if err != nil {
				t.Fatal(err)
			}
			if g := ids(got, summaryID); !slices.Equal(g, tt.want) {
				t.Errorf("got %v, want %v", g, tt.want)
			}
		})
	}

	got, _ := repo.List(context.Background(), artifact.ListParams{SortBy: "requested_at", SortOrder: "DESC", Limit: 1})
	s := got[0]
	if s.HTTPMethod != "POST" || s.FullURL != "https://b.com/2" || *s.ResponseStatusCode != 200 ||
		*s.ResponseContentType != "application/json" || *s.ResponseBodySizeBytes != 10 || !s.RequestedAt.Equal(base.Add(3*time.Second)) {
		t.Errorf("summary = %+v", s)
	}
}

func TestListTieBreakIsDeterministic(t *testing.T) {
	repo, _ := setup(t)
	var all []string
	for range 5 {
		all = append(all, insert(t, repo, seed{method: "GET", host: "a.com", path: "/"}).ID)
	}
	slices.Sort(all)
	got, err := repo.List(context.Background(), artifact.ListParams{SortBy: "requested_at", SortOrder: "ASC", Limit: 30})
	if err != nil {
		t.Fatal(err)
	}
	if g := ids(got, summaryID); !slices.Equal(g, all) {
		t.Errorf("ties not ordered by id: %v", g)
	}
}

func TestListRejectsUnknownSort(t *testing.T) {
	repo, _ := setup(t)
	if _, err := repo.List(context.Background(), artifact.ListParams{SortBy: "id; DROP TABLE http_artifacts", SortOrder: "ASC", Limit: 1}); err == nil {
		t.Fatal("expected error for unknown sort field")
	}
	if _, err := repo.List(context.Background(), artifact.ListParams{SortBy: "requested_at", SortOrder: "sideways", Limit: 1}); err == nil {
		t.Fatal("expected error for unknown sort order")
	}
}

func searchIDs(t *testing.T, repo *ArtifactRepository, q string, limit int) []string {
	t.Helper()
	got, err := repo.Search(context.Background(), artifact.SearchParams{Query: q, Limit: limit})
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	return ids(got, func(s artifact.SearchResult) string { return s.ID })
}

func TestSearch(t *testing.T) {
	repo, _ := setup(t)
	a := insert(t, repo, seed{
		method: "POST", host: "api.example.com", path: "/v1/login", at: 1 * time.Second,
		status: ptr(201), ctype: ptr("application/json"), size: ptr(int64(4242)),
		headers: `{"Authorization":"Bearer sk_live_ABC123"}`,
		reqBody: ptr(`{"password":"hunter2"}`), resp: ptr(`{"token":"eyJhbGciOi"}`),
	})
	b := insert(t, repo, seed{
		method: "GET", host: "cdn.other.org", path: "/static/app.js", at: 2 * time.Second,
		status: ptr(404), ctype: ptr("text/javascript"), size: ptr(int64(7)),
		resp: ptr(`var AWS_SECRET_ACCESS_KEY = "x"; 100% done_now`),
	})

	tests := []struct {
		q    string
		want []string
	}{
		{a.ID, []string{a.ID}},                      // id
		{strings.ToUpper(a.ID[:8]), []string{a.ID}}, // id substring, case-insensitive
		{"post", []string{a.ID}},                    // http_method
		{"EXAMPLE.COM", []string{a.ID}},             // hostname
		{"static/app", []string{b.ID}},              // endpoint_path
		{"https://cdn.other", []string{b.ID}},       // full_url
		{"2026-10-27T12:00:02", []string{b.ID}},     // requested_at
		{"2026-10-27", []string{b.ID, a.ID}},        // requested_at, newest first
		{"404", []string{b.ID}},                     // status code
		{"javascript", []string{b.ID}},              // content type
		{"4242", []string{a.ID}},                    // body size
		{"authorization", []string{a.ID}},           // header key
		{"sk_live_abc", []string{a.ID}},             // header value
		{"HUNTER2", []string{a.ID}},                 // request body
		{"eyjhbg", []string{a.ID}},                  // response body
		{"secret_access_key", []string{b.ID}},       // response body, case-insensitive
		{"100%", []string{b.ID}},                    // literal %
		{"0% d", []string{b.ID}},                    // literal % with neighbours
		{"e_n", []string{b.ID}},                     // literal _ ("done_now")
		{"%", []string{b.ID}},                       // % alone is not a wildcard
		{"_", []string{b.ID, a.ID}},                 // literal _ in both
		{"nomatch-xyz", []string{}},
		{"s", []string{b.ID, a.ID}}, // short (sub-trigram) query
	}
	for _, tt := range tests {
		t.Run(tt.q, func(t *testing.T) {
			// Random UUIDs may contain short numeric queries such as "404";
			// account for that so the test is deterministic.
			var want []string
			for _, id := range []string{b.ID, a.ID} { // newest first
				if slices.Contains(tt.want, id) || strings.Contains(id, strings.ToLower(tt.q)) {
					want = append(want, id)
				}
			}
			if got := searchIDs(t, repo, tt.q, 30); !slices.Equal(got, want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
	if got := searchIDs(t, repo, `\`, 30); len(got) != 0 {
		t.Errorf("backslash must be literal: got %v", got)
	}
	if got := searchIDs(t, repo, "2026", 1); !slices.Equal(got, []string{b.ID}) {
		t.Errorf("limit: got %v", got)
	}

	res, _ := repo.Search(context.Background(), artifact.SearchParams{Query: "hunter2", Limit: 30})
	r := res[0]
	if r.Hostname != "api.example.com" || r.EndpointPath != "/v1/login" || r.HTTPMethod != "POST" ||
		*r.ResponseStatusCode != 201 || *r.ResponseBodySizeBytes != 4242 || !r.RequestedAt.Equal(base.Add(time.Second)) {
		t.Errorf("result = %+v", r)
	}
}

func TestSearchDoesNotMatchAcrossFields(t *testing.T) {
	repo, _ := setup(t)
	insert(t, repo, seed{method: "GET", host: "abc.com", path: "/def"})
	// full_url ends with "/def" and status is NULL; "def" + separator + ...
	// must not produce a match spanning two columns.
	if got := searchIDs(t, repo, "/defGET", 30); len(got) != 0 {
		t.Errorf("cross-field match: %v", got)
	}
}

func TestSearchUsesTrigramIndex(t *testing.T) {
	repo, pool := setup(t)
	insert(t, repo, seed{method: "GET", host: "a.com", path: "/", resp: ptr("secret")})
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	// Small tables are always sequentially scanned; disabling seqscan
	// proves the query expression matches the index expression.
	if _, err := conn.Exec(ctx, "SET enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "RESET enable_seqscan")
	rows, err := conn.Query(ctx, "EXPLAIN "+strings.NewReplacer("$1", "'%secret%'", "$2", "30").Replace(searchQuery))
	if err != nil {
		t.Fatal(err)
	}
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if p := strings.Join(lines, "\n"); !strings.Contains(p, "idx_http_artifacts_search_trgm") {
		t.Errorf("search does not use trigram index:\n%s", p)
	}
}

func TestDomains(t *testing.T) {
	repo, _ := setup(t)
	insert(t, repo, seed{method: "GET", host: "a.com", path: "/", at: 1 * time.Second})
	insert(t, repo, seed{method: "GET", host: "a.com", path: "/", at: 5 * time.Second})
	insert(t, repo, seed{method: "GET", host: "a.com", path: "/", at: 2 * time.Second})
	insert(t, repo, seed{method: "GET", host: "b.com", path: "/", at: 9 * time.Second})
	insert(t, repo, seed{method: "GET", host: "c.com", path: "/", at: 3 * time.Second})
	insert(t, repo, seed{method: "GET", host: "c.com", path: "/", at: 4 * time.Second})
	insert(t, repo, seed{method: "GET", host: "d.com", path: "/", at: 9 * time.Second}) // ties b.com

	hosts := func(p artifact.DomainParams) []string {
		got, err := repo.Domains(context.Background(), p)
		if err != nil {
			t.Fatal(err)
		}
		return ids(got, func(d artifact.Domain) string { return d.Hostname })
	}
	tests := []struct {
		p    artifact.DomainParams
		want []string
	}{
		{artifact.DomainParams{SortBy: "latest_update", SortOrder: "DESC", Limit: 100}, []string{"b.com", "d.com", "a.com", "c.com"}},
		{artifact.DomainParams{SortBy: "latest_update", SortOrder: "ASC", Limit: 100}, []string{"c.com", "a.com", "b.com", "d.com"}},
		{artifact.DomainParams{SortBy: "total_artifacts", SortOrder: "DESC", Limit: 100}, []string{"a.com", "c.com", "b.com", "d.com"}},
		{artifact.DomainParams{SortBy: "total_artifacts", SortOrder: "ASC", Limit: 100}, []string{"b.com", "d.com", "c.com", "a.com"}},
		{artifact.DomainParams{SortBy: "total_artifacts", SortOrder: "DESC", Limit: 1}, []string{"a.com"}},
	}
	for _, tt := range tests {
		if got := hosts(tt.p); !slices.Equal(got, tt.want) {
			t.Errorf("%+v: got %v, want %v", tt.p, got, tt.want)
		}
	}

	got, _ := repo.Domains(context.Background(), artifact.DomainParams{SortBy: "total_artifacts", SortOrder: "DESC", Limit: 1})
	if got[0].ArtifactCount != 3 || !got[0].LastRequestedAt.Equal(base.Add(5*time.Second)) || got[0].LastRequestedAt.Location() != time.UTC {
		t.Errorf("aggregate = %+v", got[0])
	}
	if _, err := repo.Domains(context.Background(), artifact.DomainParams{SortBy: "hostname", SortOrder: "ASC", Limit: 1}); err == nil {
		t.Error("expected error for unknown sort field")
	}
}

func TestDomainsEmpty(t *testing.T) {
	repo, _ := setup(t)
	got, err := repo.Domains(context.Background(), artifact.DomainParams{SortBy: "latest_update", SortOrder: "DESC", Limit: 100})
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	_, pool := setup(t)
	testdb.New(t) // runs Migrate again
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM schema_migrations").Scan(&n); err != nil || n != 4 {
		t.Fatalf("schema_migrations rows = %d (%v)", n, err)
	}
}

func TestEscapeLike(t *testing.T) {
	for in, want := range map[string]string{`a`: `a`, `%`: `\%`, `_`: `\_`, `\`: `\\`, `a\%_b`: `a\\\%\_b`} {
		if got := escapeLike(in); got != want {
			t.Errorf("escapeLike(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDelete(t *testing.T) {
	repo, _ := setup(t)
	a := insert(t, repo, seed{method: "GET", host: "a.com", path: "/"})
	b := insert(t, repo, seed{method: "GET", host: "a.com", path: "/"})
	ctx := context.Background()

	if err := repo.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, a.ID); !errors.Is(err, artifact.ErrNotFound) {
		t.Fatalf("deleted artifact still readable: %v", err)
	}
	if err := repo.Delete(ctx, a.ID); !errors.Is(err, artifact.ErrNotFound) {
		t.Errorf("second delete: err = %v", err)
	}
	if _, err := repo.Get(ctx, b.ID); err != nil {
		t.Errorf("other artifact affected: %v", err)
	}
	if got := searchIDs(t, repo, a.ID, 30); len(got) != 0 {
		t.Errorf("deleted artifact still searchable: %v", got)
	}
}

// TestListPagination walks every sort field and direction page by page
// and checks the concatenation equals one unpaginated listing: no row is
// skipped or repeated, including NULL groups and duplicate sort values.
func TestListPagination(t *testing.T) {
	repo, _ := setup(t)
	statuses := []*int{ptr(200), nil, ptr(404), ptr(200), nil, ptr(500), ptr(200), nil}
	ctypes := []*string{ptr("text/html"), nil, ptr("application/json"), ptr("text/html"), nil, nil, ptr("a"), ptr("text/html")}
	methods := []string{"GET", "POST", "GET", "PUT", "GET", "DELETE", "POST", "GET"}
	for i := range 40 {
		var size *int64
		if i%3 != 0 {
			size = ptr(int64(i % 4 * 100))
		}
		insert(t, repo, seed{
			method: methods[i%len(methods)],
			host:   []string{"a.com", "b.com"}[i%2],
			path:   fmt.Sprintf("/%d", i%7),
			at:     time.Duration(i%10) * time.Second, // duplicate timestamps
			status: statuses[i%len(statuses)],
			ctype:  ctypes[i%len(ctypes)],
			size:   size,
		})
	}

	svc := artifact.NewService(repo, nil)
	ctx := context.Background()
	for _, field := range artifact.ListSortFields {
		for _, order := range []artifact.SortOrder{artifact.SortAsc, artifact.SortDesc} {
			for _, host := range []string{"", "a.com"} {
				for _, pageSize := range []int{1, 3, 7} {
					name := fmt.Sprintf("%s/%s/host=%s/size=%d", field, order, host, pageSize)
					all, err := repo.List(ctx, artifact.ListParams{SortBy: field, SortOrder: order, Hostname: host, Limit: 100})
					if err != nil {
						t.Fatal(err)
					}
					var walked []string
					p := artifact.ListParams{SortBy: field, SortOrder: order, Hostname: host, Limit: pageSize}
					for pages := 0; ; pages++ {
						if pages > 50 {
							t.Fatalf("%s: pagination does not terminate", name)
						}
						page, err := svc.List(ctx, p)
						if err != nil {
							t.Fatalf("%s: %v", name, err)
						}
						if len(page.Items) > pageSize {
							t.Fatalf("%s: page has %d items", name, len(page.Items))
						}
						walked = append(walked, ids(page.Items, summaryID)...)
						if page.Next == nil {
							break
						}
						p.After = page.Next
					}
					if want := ids(all, summaryID); !slices.Equal(walked, want) {
						t.Errorf("%s:\n walked %v\n   want %v", name, walked, want)
					}
				}
			}
		}
	}
}

func TestListPaginationStableUnderInserts(t *testing.T) {
	repo, _ := setup(t)
	for i := range 6 {
		insert(t, repo, seed{method: "GET", host: "a.com", path: "/", at: time.Duration(i) * time.Second})
	}
	svc := artifact.NewService(repo, nil)
	ctx := context.Background()
	p := artifact.ListParams{SortBy: "requested_at", SortOrder: artifact.SortDesc, Limit: 3}
	first, err := svc.List(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	// New artifacts arrive at the top while the client is paging.
	for i := range 5 {
		insert(t, repo, seed{method: "GET", host: "a.com", path: "/", at: time.Hour + time.Duration(i)*time.Second})
	}
	p.After = first.Next
	second, err := svc.List(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range append(first.Items, second.Items...) {
		if seen[s.ID] {
			t.Fatalf("duplicate across pages: %s", s.ID)
		}
		seen[s.ID] = true
		if s.RequestedAt.After(base.Add(time.Minute)) {
			t.Errorf("row inserted after paging began leaked into later page")
		}
	}
	if len(seen) != 6 || second.Next != nil {
		t.Errorf("got %d rows, next=%v", len(seen), second.Next)
	}
}
