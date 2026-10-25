// Package poll owns bounded Jenkins observation, durable persistence,
// aggregate state delivery, and trigger correlation.
package poll

import (
	"context"
	"sync"
	"time"

	"charm.land/log/v2"
	"github.com/atm1020/gojenkins"
	"github.com/atm1020/jenktui/src/store"
)

// Config contains the scheduler durations and concurrency bound. Durations are
// used directly so the poll core does not need to know how config files encode
// them.
type Config struct {
	PollInterval    time.Duration
	MaxParallelJobs int
	RequestTimeout  time.Duration
	RetryBase       time.Duration
	RetryMax        time.Duration
}

func withDefaults(cfg Config) Config {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 3 * time.Second
	}
	if cfg.MaxParallelJobs <= 0 {
		cfg.MaxParallelJobs = 2
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 15 * time.Second
	}
	if cfg.RetryBase <= 0 {
		cfg.RetryBase = 500 * time.Millisecond
	}
	if cfg.RetryMax <= 0 {
		cfg.RetryMax = 30 * time.Second
	}
	if cfg.RetryMax < cfg.RetryBase {
		cfg.RetryMax = cfg.RetryBase
	}
	return cfg
}

// Manager owns the poll lifecycle. The application creates one manager for
// the current Jenkins server, calls Start once, and calls Stop once before
// replacing it or exiting. Duplicate lifecycle calls are programmer errors and
// are not hidden behind defensive state.
type Manager struct {
	config Config

	ctx      context.Context
	cancel   context.CancelFunc
	loopWG   sync.WaitGroup
	watches  watchRegistry
	executor jobExecutor
	wake     chan struct{}
	results  chan workResult

	repo                repository
	windowsMu           sync.Mutex
	windows             map[string]jobWindow
	aggregateMu         sync.Mutex
	pendingSnapshots    map[Target]Snapshot
	pendingFailures     map[Target]PollFailure
	pendingRunsChanged  bool
	deliveryReady       chan struct{}
	deliveries          chan Delivery
	deliveryWG          sync.WaitGroup
	completionMu        sync.Mutex
	inFlightCompletions map[CompletionKey]struct{}
	internalMu          sync.Mutex
	internalWatches     map[Target]*Watch
	queue               queueResolver
}

// NewManager constructs the production manager for one initialized Jenkins server.
func NewManager(jenkins *gojenkins.Jenkins, runStore *store.Store, cfg Config) *Manager {
	m := newManager(cfg, nil)
	if runStore != nil {
		m.repo = storeRepository{store: runStore}
	}
	m.queue = jenkinsQueueResolver{jenkins: jenkins}
	m.executor = pollingExecutor{manager: m, source: jenkinsSource{jenkins: jenkins}}
	return m
}

func newManager(cfg Config, executor jobExecutor) *Manager {
	cfg = withDefaults(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		config:              cfg,
		ctx:                 ctx,
		cancel:              cancel,
		watches:             newWatchRegistry(),
		executor:            executor,
		wake:                make(chan struct{}, 1),
		results:             make(chan workResult, cfg.MaxParallelJobs),
		windows:             make(map[string]jobWindow),
		pendingSnapshots:    make(map[Target]Snapshot),
		pendingFailures:     make(map[Target]PollFailure),
		deliveryReady:       make(chan struct{}, 1),
		deliveries:          make(chan Delivery, 1),
		inFlightCompletions: make(map[CompletionKey]struct{}),
		internalWatches:     make(map[Target]*Watch),
	}
}

// Start launches the scheduler and aggregate delivery loops.
func (m *Manager) Start() {
	m.loadDurableDemand()
	m.deliveryWG.Add(1)
	go m.deliveryLoop()
	m.loopWG.Add(1)
	go m.run()
	m.signalDelivery()
	log.Info("poll: manager started",
		"interval", m.config.PollInterval,
		"maxParallel", m.config.MaxParallelJobs,
		"timeout", m.config.RequestTimeout,
		"retryBase", m.config.RetryBase,
		"retryMax", m.config.RetryMax,
	)
}

// Stop cancels and joins the scheduler, workers, and delivery loop.
func (m *Manager) Stop() {
	m.cancel()
	m.loopWG.Wait()
	m.deliveryWG.Wait()
	close(m.deliveries)
	log.Info("poll: manager stopped")
}

func (m *Manager) run() {
	defer m.loopWG.Done()
	m.runScheduler()
}
