package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/reconvault/reconvault-dns-autoscan/internal/dnsscan"
	"github.com/reconvault/reconvault-dns-autoscan/internal/domainscan"
	"github.com/reconvault/reconvault-dns-autoscan/internal/store"
)

func TestNextDailyRun(t *testing.T) {
	tests := []struct{ now, want string }{
		{"2026-10-28T13:45:00Z", "2026-10-29T00:00:00Z"},
		{"2026-10-28T00:00:00Z", "2026-10-29T00:00:00Z"},
		{"2026-10-28T23:59:59Z", "2026-10-29T00:00:00Z"},
		{"2026-12-31T08:00:00Z", "2027-01-01T00:00:00Z"},
		{"2026-10-29T02:00:00+07:00", "2026-10-29T00:00:00Z"}, // 2026-10-28 19:00 UTC
	}
	for _, tt := range tests {
		now, _ := time.Parse(time.RFC3339, tt.now)
		want, _ := time.Parse(time.RFC3339, tt.want)
		if got := NextDailyRun(now); !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("NextDailyRun(%s) = %s, want %s", tt.now, got, tt.want)
		}
	}
}

// fakeStore is an in-memory Store.
type fakeStore struct {
	mu            sync.Mutex
	pendingDomain []string
	pendingDNS    []string
	scanned       []string
	incomplete    []string
	upserted      []string
	applied       []string
	failUpsert    map[string]bool
	lastCompleted *time.Time
	started       int
	completed     int
	lockHeld      bool
}

func (f *fakeStore) take(list *[]string, limit int) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := min(limit, len(*list))
	return append([]string(nil), (*list)[:n]...)
}

func (f *fakeStore) PendingDomainHosts(_ context.Context, limit int) ([]string, error) {
	return f.take(&f.pendingDomain, limit), nil
}
func (f *fakeStore) PendingDNSTargets(_ context.Context, limit int) ([]string, error) {
	return f.take(&f.pendingDNS, limit), nil
}

func page(all []string, after string, limit int) []string {
	var out []string
	for _, h := range all {
		if h > after && len(out) < limit {
			out = append(out, h)
		}
	}
	return out
}

func (f *fakeStore) ScannedDNSTargets(_ context.Context, after string, limit int) ([]string, error) {
	return page(f.scanned, after, limit), nil
}
func (f *fakeStore) IncompleteDomains(_ context.Context, after string, limit int) ([]string, error) {
	return page(f.incomplete, after, limit), nil
}
func (f *fakeStore) UpsertDomain(_ context.Context, d domainscan.Domain) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failUpsert[d.Hostname] {
		return errors.New("db down")
	}
	f.upserted = append(f.upserted, d.Hostname)
	f.pendingDomain = slices.DeleteFunc(f.pendingDomain, func(h string) bool { return h == d.Hostname })
	return nil
}
func (f *fakeStore) ApplyDNSOutcomes(_ context.Context, host string, _ []dnsscan.Outcome, _ time.Time) (store.ApplyResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.applied = append(f.applied, host)
	f.pendingDNS = slices.DeleteFunc(f.pendingDNS, func(h string) bool { return h == host })
	return store.ApplyResult{}, nil
}
func (f *fakeStore) LastCompleted(context.Context, string) (*time.Time, error) {
	return f.lastCompleted, nil
}
func (f *fakeStore) JobStarted(context.Context, string, time.Time) error { f.started++; return nil }
func (f *fakeStore) JobCompleted(_ context.Context, _ string, at time.Time) error {
	f.completed++
	f.lastCompleted = &at
	return nil
}
func (f *fakeStore) TryLock(context.Context, string) (func(), bool, error) {
	if f.lockHeld {
		return nil, false, nil
	}
	return func() {}, true, nil
}

type fakeDomain struct{ calls atomic.Int64 }

func (f *fakeDomain) Scan(_ context.Context, host string) domainscan.Domain {
	f.calls.Add(1)
	return domainscan.Domain{Hostname: host, RootDomain: domainscan.RootDomain(host)}
}

type fakeDNS struct {
	inFlight, maxInFlight atomic.Int64
	delay                 time.Duration
}

func (f *fakeDNS) Scan(context.Context, string) []dnsscan.Outcome {
	n := f.inFlight.Add(1)
	for {
		m := f.maxInFlight.Load()
		if n <= m || f.maxInFlight.CompareAndSwap(m, n) {
			break
		}
	}
	time.Sleep(f.delay)
	f.inFlight.Add(-1)
	return nil
}

func newScheduler(st Store, opts Options) (*Scheduler, *fakeDomain, *fakeDNS) {
	d, n := &fakeDomain{}, &fakeDNS{}
	return New(st, d, n, opts, slog.New(slog.NewTextHandler(io.Discard, nil))), d, n
}

func sorted(s []string) []string { s = append([]string(nil), s...); sort.Strings(s); return s }

func TestRunActiveDrainsBothPhases(t *testing.T) {
	st := &fakeStore{
		pendingDomain: []string{"a.test", "b.test", "c.test", "d.test", "e.test"},
		pendingDNS:    []string{"a.test", "b.test", "c.test"},
	}
	s, d, _ := newScheduler(st, Options{BatchSize: 2, Concurrency: 2})
	if err := s.RunActive(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := sorted(st.upserted); !slices.Equal(got, []string{"a.test", "b.test", "c.test", "d.test", "e.test"}) || d.calls.Load() != 5 {
		t.Fatalf("domains: %v (%d scans)", got, d.calls.Load())
	}
	if got := sorted(st.applied); !slices.Equal(got, []string{"a.test", "b.test", "c.test"}) {
		t.Fatalf("dns: %v", got)
	}
}

func TestRunActiveStopsWhenNoProgress(t *testing.T) {
	st := &fakeStore{
		pendingDomain: []string{"bad1.test", "bad2.test"},
		failUpsert:    map[string]bool{"bad1.test": true, "bad2.test": true},
	}
	s, d, _ := newScheduler(st, Options{BatchSize: 2, Concurrency: 2})
	done := make(chan struct{})
	go func() { _ = s.RunActive(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunActive spun on persistent failures")
	}
	if d.calls.Load() != 2 {
		t.Errorf("scans = %d, want one attempt per host", d.calls.Load())
	}
}

func TestConcurrencyIsBounded(t *testing.T) {
	var hosts []string
	for i := range 20 {
		hosts = append(hosts, string(rune('a'+i))+".test")
	}
	st := &fakeStore{pendingDNS: hosts}
	s, _, n := newScheduler(st, Options{BatchSize: 20, Concurrency: 3})
	n.delay = 20 * time.Millisecond
	if err := s.RunActive(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m := n.maxInFlight.Load(); m > 3 || m < 2 {
		t.Errorf("max concurrent scans = %d, want <= 3", m)
	}
}

func TestRunDailyPagesEverythingAndRecordsCompletion(t *testing.T) {
	st := &fakeStore{
		scanned:    []string{"a.test", "b.test", "c.test", "d.test", "e.test"},
		incomplete: []string{"x.test", "y.test", "z.test"},
	}
	s, d, _ := newScheduler(st, Options{BatchSize: 2, Concurrency: 2})
	if err := s.RunDaily(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := sorted(st.applied); !slices.Equal(got, st.scanned) {
		t.Fatalf("refreshed %v", got)
	}
	if d.calls.Load() != 3 || st.started != 1 || st.completed != 1 {
		t.Fatalf("domains retried %d, started %d, completed %d", d.calls.Load(), st.started, st.completed)
	}
}

func TestDailyMissed(t *testing.T) {
	now := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		last *time.Time
		want bool
	}{
		{"never ran", nil, false},
		{"ran after last midnight", ptr(time.Date(2026, 10, 28, 0, 5, 0, 0, time.UTC)), false},
		{"ran at last midnight", ptr(time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)), false},
		{"last ran the day before", ptr(time.Date(2026, 10, 27, 0, 5, 0, 0, time.UTC)), true},
	}
	for _, tt := range tests {
		st := &fakeStore{lastCompleted: tt.last}
		s, _, _ := newScheduler(st, Options{BatchSize: 1, Concurrency: 1})
		s.now = func() time.Time { return now }
		if got := s.dailyMissed(context.Background()); got != tt.want {
			t.Errorf("%s: got %v", tt.name, got)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestWithLockSkipsWhenHeldElsewhere(t *testing.T) {
	st := &fakeStore{lockHeld: true, pendingDomain: []string{"a.test"}}
	s, d, _ := newScheduler(st, Options{BatchSize: 1, Concurrency: 1})
	s.withLock(context.Background(), jobActive, s.RunActive)
	if d.calls.Load() != 0 {
		t.Fatal("job ran without the lock")
	}
}

func TestCancelledScansAreNotPersisted(t *testing.T) {
	st := &fakeStore{pendingDomain: []string{"a.test"}}
	s, _, _ := newScheduler(st, Options{BatchSize: 1, Concurrency: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.scanDomain(ctx, "a.test"); !errors.Is(err, context.Canceled) || len(st.upserted) != 0 {
		t.Fatalf("err = %v, upserted = %v", err, st.upserted)
	}
}
