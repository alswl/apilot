package integrations

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// WorkbenchGitHubRate coordinates all repositories belonging to an account.
type WorkbenchGitHubRate struct {
	mu             sync.Mutex
	accounts       map[int64]*githubRateAccount
	global         chan struct{}
	polls          map[githubPollKey]time.Time
	persistBackoff func(int64, time.Time)
}

// SetBackoffPersistence must be set before requests begin.
func (r *WorkbenchGitHubRate) SetBackoffPersistence(persist func(int64, time.Time)) {
	r.persistBackoff = persist
}

func (r *WorkbenchGitHubRate) RestoreBackoff(id int64, until time.Time) {
	a := r.account(id)
	r.mu.Lock()
	if until.After(a.blockedUntil) {
		a.blockedUntil = until
	}
	r.mu.Unlock()
}

type githubPollKey struct {
	accountID    int64
	connectionID string
}

type githubRateAccount struct {
	gate         chan struct{}
	nextWrite    time.Time
	blockedUntil time.Time
	backoff      time.Duration
}

func NewWorkbenchGitHubRate(maxConcurrent int) *WorkbenchGitHubRate {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &WorkbenchGitHubRate{accounts: make(map[int64]*githubRateAccount), global: make(chan struct{}, maxConcurrent), polls: make(map[githubPollKey]time.Time)}
}

// PollDelay reports the server-requested minimum interval for this repository.
func (r *WorkbenchGitHubRate) PollDelay(accountID int64, connectionID string, now time.Time) time.Duration {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	until := r.polls[githubPollKey{accountID, connectionID}]
	if !until.After(now) {
		return 0
	}
	return until.Sub(now)
}

// BackoffDelay exposes the shared account wait without occupying a worker.
func (r *WorkbenchGitHubRate) BackoffDelay(accountID int64, now time.Time) time.Duration {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if account := r.accounts[accountID]; account != nil && account.blockedUntil.After(now) {
		return account.blockedUntil.Sub(now)
	}
	return 0
}

func (r *WorkbenchGitHubRate) observePoll(accountID int64, connectionID string, header http.Header) time.Time {
	seconds, err := strconv.ParseInt(header.Get("X-Poll-Interval"), 10, 32)
	if err != nil || seconds <= 0 {
		return time.Time{}
	}
	until := time.Now().Add(time.Duration(seconds) * time.Second)
	r.mu.Lock()
	defer r.mu.Unlock()
	key := githubPollKey{accountID, connectionID}
	if until.After(r.polls[key]) {
		r.polls[key] = until
	}
	return until
}

func (r *WorkbenchGitHubRate) account(id int64) *githubRateAccount {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.accounts[id]
	if a == nil {
		a = &githubRateAccount{gate: make(chan struct{}, 1)}
		r.accounts[id] = a
	}
	return a
}

func (r *WorkbenchGitHubRate) acquire(ctx context.Context, id int64, write bool) (func(), error) {
	a := r.account(id)
	select {
	case a.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	releaseAccount := func() { <-a.gate }
	r.mu.Lock()
	until := a.blockedUntil
	if write && a.nextWrite.After(until) {
		until = a.nextWrite
	}
	r.mu.Unlock()
	if wait := time.Until(until); wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			releaseAccount()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	select {
	case r.global <- struct{}{}:
	case <-ctx.Done():
		releaseAccount()
		return nil, ctx.Err()
	}
	return func() {
		if write {
			r.mu.Lock()
			a.nextWrite = time.Now().Add(time.Second)
			r.mu.Unlock()
		}
		<-r.global
		releaseAccount()
	}, nil
}

func (r *WorkbenchGitHubRate) observe(id int64, resp *http.Response, secondary ...bool) {
	limited := resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden && (len(secondary) > 0 && secondary[0] || resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-RateLimit-Remaining") == "0"))
	r.mu.Lock()
	a := r.accounts[id]
	if a == nil {
		r.mu.Unlock()
		return
	}
	if !limited {
		if resp.StatusCode < 400 {
			a.backoff = 0
		}
		r.mu.Unlock()
		return
	}
	now := time.Now()
	until := now
	if value := resp.Header.Get("Retry-After"); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds > 0 {
			until = now.Add(time.Duration(seconds) * time.Second)
		} else if date, err := http.ParseTime(value); err == nil && date.After(until) {
			until = date
		}
	}
	if value := resp.Header.Get("X-RateLimit-Reset"); value != "" && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && time.Unix(seconds, 0).After(until) {
			until = time.Unix(seconds, 0)
		}
	}
	if !until.After(now) {
		if a.backoff == 0 {
			a.backoff = time.Minute
		} else {
			a.backoff = min(a.backoff*2, 15*time.Minute)
		}
		until = now.Add(a.backoff + time.Duration(rand.Int64N(int64(a.backoff/5)+1)))
	}
	if until.After(a.blockedUntil) {
		a.blockedUntil = until
	}
	until = a.blockedUntil
	persist := r.persistBackoff
	r.mu.Unlock()
	if persist != nil {
		persist(id, until)
	}
}

type githubWriteLease struct{}

// AcquireWrite waits before the durable operation is claimed for dispatch.
func (c *WorkbenchGitHubClient) AcquireWrite(ctx context.Context) (context.Context, func(), error) {
	if c.rate == nil {
		return ctx, func() {}, nil
	}
	release, err := c.rate.acquire(ctx, c.accountID, true)
	if err != nil {
		return ctx, nil, err
	}
	return context.WithValue(ctx, githubWriteLease{}, c), release, nil
}
