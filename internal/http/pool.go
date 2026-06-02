package http

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Job is a single unit of work submitted to the Pool.
type Job struct {
	Req     *Request
	attempt int // number of retries already consumed (managed internally)
}

// Result is the outcome of one Job: either Resp or Err will be set.
type Result struct {
	Req  *Request
	Resp *Response
	Err  error
}

// PoolConfig holds creation parameters for a Pool.
type PoolConfig struct {
	// MaxConnPerHost limits concurrent connections to any single host.
	MaxConnPerHost int
	// MaxParallelHosts limits how many distinct hosts may be active at once.
	// Total worker goroutines = MaxConnPerHost × MaxParallelHosts.
	MaxParallelHosts int
	// Client is the HTTP client used to execute requests.
	Client *Client
	// Limiter applies per-host rate limiting and 429 backoff (may be nil).
	Limiter *RateLimiter
}

// Pool is a bounded goroutine worker pool that executes HTTP requests with
// per-host connection limiting, rate control, and 429 auto-backoff.
type Pool struct {
	client   *Client
	limiter  *RateLimiter
	jobs     chan *Job
	results  chan *Result
	submitWg sync.WaitGroup // in-flight Submit goroutines
	workerWg sync.WaitGroup // running worker goroutines
	once     sync.Once
	ctx      context.Context
	cancel   context.CancelFunc
	hostSems sync.Map // map[string]chan struct{} — per-host semaphores
	maxConn  int
}

// NewPool creates a Pool and starts its worker goroutines.
func NewPool(cfg PoolConfig) *Pool {
	if cfg.MaxConnPerHost <= 0 {
		cfg.MaxConnPerHost = 5
	}
	if cfg.MaxParallelHosts <= 0 {
		cfg.MaxParallelHosts = 10
	}
	workerCount := cfg.MaxConnPerHost * cfg.MaxParallelHosts

	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{
		client:  cfg.Client,
		limiter: cfg.Limiter,
		jobs:    make(chan *Job, workerCount*4),
		results: make(chan *Result, workerCount*4),
		ctx:     ctx,
		cancel:  cancel,
		maxConn: cfg.MaxConnPerHost,
	}

	p.workerWg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go p.worker()
	}
	return p
}

// Submit enqueues req for processing. It never blocks the caller; the job is
// queued asynchronously. Submits after Shutdown are silently dropped.
func (p *Pool) Submit(req *Request) {
	p.submitWg.Add(1)
	go func() {
		defer p.submitWg.Done()
		select {
		case p.jobs <- &Job{Req: req}:
		case <-p.ctx.Done():
		}
	}()
}

// Results returns the channel on which processed Results arrive.
// The channel is closed by Shutdown once all in-flight work is drained.
func (p *Pool) Results() <-chan *Result {
	return p.results
}

// Shutdown signals the pool to stop accepting new jobs, waits for all
// in-flight requests to complete, flushes results, and closes the Results
// channel. It is safe to call Shutdown multiple times.
func (p *Pool) Shutdown() {
	p.once.Do(func() {
		p.cancel()        // abort pending Submit goroutines and interruptible sleeps
		p.submitWg.Wait() // wait until no more jobs are being enqueued
		close(p.jobs)     // tell workers: no more jobs after the ones already queued
		p.workerWg.Wait() // drain in-flight requests
		close(p.results)  // signal consumers: no more results
	})
}

// WatchSignals starts a background goroutine that calls Shutdown when the
// process receives SIGINT or SIGTERM, enabling clean exit on Ctrl-C.
func (p *Pool) WatchSignals() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer signal.Stop(ch)
		select {
		case <-ch:
			p.Shutdown()
		case <-p.ctx.Done():
		}
	}()
}

// worker drains the jobs channel, processing each job until the channel is closed.
func (p *Pool) worker() {
	defer p.workerWg.Done()
	for job := range p.jobs {
		result := p.process(job)
		select {
		case p.results <- result:
		case <-p.ctx.Done():
			// Pool is shutting down; still emit the result if there is room,
			// then exit so workerWg.Wait() can unblock.
			select {
			case p.results <- result:
			default:
			}
			// Drain remaining jobs without executing them so close(p.jobs) unblocks.
			for range p.jobs { //nolint:revive
			}
			return
		}
	}
}

// process executes a single job, retrying on 429 responses per the rate limiter.
func (p *Pool) process(job *Job) *Result {
	host := job.Req.Target.Host

	// Acquire the per-host semaphore before making any network call.
	sem := p.hostSem(host)
	select {
	case sem <- struct{}{}:
	case <-p.ctx.Done():
		return &Result{Req: job.Req, Err: context.Canceled}
	}
	defer func() { <-sem }()

	for attempt := job.attempt; ; attempt++ {
		// Honour per-host inter-request delay.
		if p.limiter != nil {
			if err := p.limiter.Wait(host, p.ctx); err != nil {
				return &Result{Req: job.Req, Err: err}
			}
		}

		resp, err := p.client.DoContext(p.ctx, job.Req)
		if err != nil {
			return &Result{Req: job.Req, Err: err}
		}

		if resp.StatusCode == 429 && p.limiter != nil {
			delay, ok := p.limiter.On429(host, attempt)
			if ok {
				select {
				case <-time.After(delay):
					continue // retry after backoff
				case <-p.ctx.Done():
					return &Result{Req: job.Req, Err: context.Canceled}
				}
			}
			// max retries exceeded — return the 429 as-is
		}

		if p.limiter != nil && resp.StatusCode != 429 {
			p.limiter.ResetBackoff(host)
		}

		return &Result{Req: job.Req, Resp: resp}
	}
}

// hostSem returns the per-host semaphore channel, creating it on first access.
func (p *Pool) hostSem(host string) chan struct{} {
	v, _ := p.hostSems.LoadOrStore(host, make(chan struct{}, p.maxConn))
	return v.(chan struct{})
}
