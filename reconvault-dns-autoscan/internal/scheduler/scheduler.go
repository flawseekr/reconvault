// Package scheduler runs the active and daily scan jobs.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/reconvault/reconvault-dns-autoscan/internal/dnsscan"
	"github.com/reconvault/reconvault-dns-autoscan/internal/domainscan"
	"github.com/reconvault/reconvault-dns-autoscan/internal/store"
)

const (
	jobActive = "active"
	jobDaily  = "daily"
)

// Store is the persistence the scheduler needs.
type Store interface {
	PendingDomainHosts(ctx context.Context, limit int) ([]string, error)
	PendingDNSTargets(ctx context.Context, limit int) ([]string, error)
	ScannedDNSTargets(ctx context.Context, after string, limit int) ([]string, error)
	ScannedDomains(ctx context.Context, after string, limit int) ([]string, error)
	UpsertDomain(ctx context.Context, d domainscan.Domain) error
	ApplyDNSOutcomes(ctx context.Context, host string, outcomes []dnsscan.Outcome, now time.Time) (store.ApplyResult, error)
	LastCompleted(ctx context.Context, job string) (*time.Time, error)
	JobStarted(ctx context.Context, job string, at time.Time) error
	JobCompleted(ctx context.Context, job string, at time.Time) error
	TryLock(ctx context.Context, name string) (unlock func(), ok bool, err error)
}

// DomainScanner scans one hostname for the domains table.
type DomainScanner interface {
	Scan(ctx context.Context, host string) domainscan.Domain
}

// DNSScanner scans one hostname's DNS records.
type DNSScanner interface {
	Scan(ctx context.Context, host string) []dnsscan.Outcome
}

// Options configure the Scheduler.
type Options struct {
	ActiveInterval time.Duration
	BatchSize      int
	Concurrency    int
}

// Scheduler runs the jobs until its context ends.
type Scheduler struct {
	store  Store
	domain DomainScanner
	dns    DNSScanner
	opts   Options
	log    *slog.Logger
	now    func() time.Time

	// lastActive is the completion time of the last active pass (unix
	// seconds), used by the health check.
	lastActive atomic.Int64
}

// New returns a Scheduler.
func New(st Store, domain DomainScanner, dns DNSScanner, opts Options, log *slog.Logger) *Scheduler {
	return &Scheduler{store: st, domain: domain, dns: dns, opts: opts, log: log, now: time.Now}
}

// Run starts both jobs and blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Go(func() { s.activeLoop(ctx) })
	wg.Go(func() { s.dailyLoop(ctx) })
	wg.Wait()
}

// LastActivePass reports when the last active pass finished.
func (s *Scheduler) LastActivePass() time.Time {
	sec := s.lastActive.Load()
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

func (s *Scheduler) activeLoop(ctx context.Context) {
	ticker := time.NewTicker(s.opts.ActiveInterval)
	defer ticker.Stop()
	for {
		s.withLock(ctx, jobActive, s.RunActive)
		s.lastActive.Store(s.now().Unix())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) dailyLoop(ctx context.Context) {
	if s.dailyMissed(ctx) {
		s.log.Info("daily refresh missed while stopped; running now")
		s.withLock(ctx, jobDaily, s.RunDaily)
	}
	for {
		next := NextDailyRun(s.now())
		s.log.Info("daily refresh scheduled", "at", next)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
		}
		s.withLock(ctx, jobDaily, s.RunDaily)
	}
}

// NextDailyRun returns the next 00:00 UTC strictly after now.
func NextDailyRun(now time.Time) time.Time {
	u := now.UTC()
	return time.Date(u.Year(), u.Month(), u.Day()+1, 0, 0, 0, 0, time.UTC)
}

// dailyMissed reports whether the most recent 00:00 UTC passed without a
// completed daily run. A job that never ran has nothing to catch up on:
// the active job performs the initial scans.
func (s *Scheduler) dailyMissed(ctx context.Context) bool {
	last, err := s.store.LastCompleted(ctx, jobDaily)
	if err != nil {
		s.log.Error("read daily job state", "error", err)
		return false
	}
	if last == nil {
		return false
	}
	return last.Before(NextDailyRun(s.now()).AddDate(0, 0, -1))
}

func (s *Scheduler) withLock(ctx context.Context, job string, run func(context.Context) error) {
	unlock, ok, err := s.store.TryLock(ctx, job)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error("acquire job lock", "job", job, "error", err)
		}
		return
	}
	if !ok {
		s.log.Debug("job running on another instance", "job", job)
		return
	}
	defer unlock()
	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.log.Error("job failed", "job", job, "error", err)
	}
}

// RunActive scans new hostnames from http_artifacts into domains, then DNS
// records of domains (hostnames and root domains) not yet scanned.
func (s *Scheduler) RunActive(ctx context.Context) error {
	domains, err := s.drain(ctx, s.store.PendingDomainHosts, s.scanDomain)
	if err != nil {
		return err
	}
	records, err := s.drain(ctx, s.store.PendingDNSTargets, s.scanDNS)
	if err != nil {
		return err
	}
	if domains+records > 0 {
		s.log.Info("active pass completed", "domains", domains, "dns_targets", records)
	}
	return nil
}

// RunDaily refreshes the DNS records of every scanned target, then the root
// domain, ASN and WAF status of every domain. UpsertDomain keeps a
// previously verified ASN or WAF value when a rescan can't determine it.
func (s *Scheduler) RunDaily(ctx context.Context) error {
	started := s.now()
	if err := s.store.JobStarted(ctx, jobDaily, started); err != nil {
		return err
	}
	records, err := s.pages(ctx, s.store.ScannedDNSTargets, s.scanDNS)
	if err != nil {
		return err
	}
	domains, err := s.pages(ctx, s.store.ScannedDomains, s.scanDomain)
	if err != nil {
		return err
	}
	s.log.Info("daily refresh completed", "dns_targets", records, "domains", domains, "duration", s.now().Sub(started).Round(time.Second))
	return s.store.JobCompleted(ctx, jobDaily, s.now())
}

// drain processes batches until none remain. A batch without any success
// ends the pass, so a persistent failure cannot spin.
func (s *Scheduler) drain(ctx context.Context, next func(context.Context, int) ([]string, error), scan func(context.Context, string) error) (int, error) {
	total := 0
	for {
		hosts, err := next(ctx, s.opts.BatchSize)
		if err != nil {
			return total, err
		}
		if len(hosts) == 0 {
			return total, nil
		}
		ok := s.forEach(ctx, hosts, scan)
		total += ok
		if ok == 0 {
			return total, ctx.Err()
		}
		if len(hosts) < s.opts.BatchSize {
			return total, nil
		}
	}
}

// pages walks every page of a keyset-paginated listing.
func (s *Scheduler) pages(ctx context.Context, next func(context.Context, string, int) ([]string, error), scan func(context.Context, string) error) (int, error) {
	total, after := 0, ""
	for {
		hosts, err := next(ctx, after, s.opts.BatchSize)
		if err != nil {
			return total, err
		}
		if len(hosts) == 0 {
			return total, nil
		}
		total += s.forEach(ctx, hosts, scan)
		if err := ctx.Err(); err != nil {
			return total, err
		}
		after = hosts[len(hosts)-1]
	}
}

// forEach runs scan for every host with bounded concurrency and returns
// the number of successes.
func (s *Scheduler) forEach(ctx context.Context, hosts []string, scan func(context.Context, string) error) int {
	var (
		ok  atomic.Int64
		wg  sync.WaitGroup
		sem = make(chan struct{}, s.opts.Concurrency)
	)
	for _, h := range hosts {
		select {
		case <-ctx.Done():
		case sem <- struct{}{}:
			wg.Go(func() {
				defer func() { <-sem }()
				if err := scan(ctx, h); err != nil {
					if ctx.Err() == nil {
						s.log.Warn("scan failed", "hostname", h, "error", err)
					}
					return
				}
				ok.Add(1)
			})
			continue
		}
		break
	}
	wg.Wait()
	return int(ok.Load())
}

func (s *Scheduler) scanDomain(ctx context.Context, host string) error {
	d := s.domain.Scan(ctx, host)
	if err := ctx.Err(); err != nil {
		return err // do not persist a scan cut short by shutdown
	}
	return s.store.UpsertDomain(ctx, d)
}

func (s *Scheduler) scanDNS(ctx context.Context, host string) error {
	outcomes := s.dns.Scan(ctx, host)
	if err := ctx.Err(); err != nil {
		return err
	}
	res, err := s.store.ApplyDNSOutcomes(ctx, host, outcomes, s.now())
	if err != nil {
		return err
	}
	s.log.Debug("dns scanned", "hostname", host, "new_versions", res.NewVersions, "unchanged", res.Unchanged, "absent", res.Absent, "failed", res.Failed)
	return nil
}
