package context

import (
	stdctx "context"
	"sync"
	"time"

	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/stagegraph"
	"github.com/atm1020/jenktui/src/store"

	pocApi "github.com/atm1020/gojenkins"
)

type ActiveView int

const (
	Main ActiveView = iota
	GlobalSearch
	GlobalSearchResults
	SearchResultsView
	JobView
	Poc
)

// ServerConfig holds one Jenkins server connection, loaded from server.json.
type ServerConfig struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	JenkinsURL string `json:"jenkins_url"`
	Username   string `json:"username"`
	APIToken   string `json:"api_token"`
	CACertPath string `json:"ca_cert_path,omitempty"`
}

// ServerDB is the top-level server.json structure.
type ServerDB struct {
	Servers []ServerConfig `json:"servers"`
}

// Config holds application settings (theme, keybindings, etc.), loaded from config.json
type NotificationConfig struct {
	DesktopEnabled bool `json:"desktop_enabled"`
	SoundEnabled   bool `json:"sound_enabled"`
}

type Config struct {
	PollInterval        int `json:"poll_interval"`
	PollMaxParallelJobs int `json:"poll_max_parallel_jobs"`
	// JenkinsRequestTimeoutSeconds bounds ordinary Jenkins API calls and poll
	// requests. The legacy JSON key is retained for config compatibility.
	JenkinsRequestTimeoutSeconds int                `json:"poll_request_timeout_seconds"`
	Theme                        string             `json:"theme"`
	Notification                 NotificationConfig `json:"notification"`
	KeyMapConfig                 KeyMapConfig       `json:"key_map"`
	// LastServerID is the ID of the last connected-to server (see ServerConfig.ID).
	// Used to skip the server selector dialog on startup when it's still valid.
	LastServerID string `json:"last_server_id"`
}

const (
	defaultRequestTimeout  = 15 * time.Second
	defaultDownloadTimeout = 10 * time.Minute
)

type ProgramContext struct {
	Jenkins           *pocApi.Jenkins
	ServerConfig      *ServerConfig               // Currently selected server
	Servers           []ServerConfig              // All configured servers
	Config            *Config                     // Application configuration
	RunStore          *store.Store                // Persistent store for user-triggered builds
	PollManager       *poll.Manager               // Active polling manager (set after Jenkins init)
	GraphCapabilities *stagegraph.CapabilityCache // Per-server pipeline-graph-view capability cache
	graphCapMu        sync.Mutex

	// Base directories
	ConfigDir string
	DataDir   string
	CacheDir  string // Flat structure - all temp files here

	// Data subdirectories (for permanent storage)
	DataLogsDir      string // ~/.local/share/JenkTui/logs
	DataArtifactsDir string // ~/.local/share/JenkTui/artifacts
}

// GraphCapabilityCache returns the thread-safe per-server pipeline-graph-view
// capability cache, creating it lazily if not yet initialized.
func (c *ProgramContext) GraphCapabilityCache() *stagegraph.CapabilityCache {
	if c == nil {
		return nil
	}
	c.graphCapMu.Lock()
	defer c.graphCapMu.Unlock()
	if c.GraphCapabilities == nil {
		c.GraphCapabilities = stagegraph.NewCapabilityCache()
	}
	return c.GraphCapabilities
}

// ResetGraphCapabilities clears cached pipeline-graph-view probe results (for
// example when switching Jenkins servers).
func (c *ProgramContext) ResetGraphCapabilities() {
	if c == nil {
		return
	}
	c.graphCapMu.Lock()
	cache := c.GraphCapabilities
	c.graphCapMu.Unlock()
	if cache != nil {
		cache.Reset()
	}
}

// GraphServerKey returns a stable key scoping graph capabilities to the active
// Jenkins server.
func (c *ProgramContext) GraphServerKey() string {
	if c == nil {
		return ""
	}
	if c.ServerConfig != nil && c.ServerConfig.ID != "" {
		return c.ServerConfig.ID
	}
	if c.Jenkins != nil {
		return c.Jenkins.Server
	}
	return ""
}

// RequestContext bounds a single Jenkins API call. Callers own the CancelFunc.
func (c *ProgramContext) RequestContext() (stdctx.Context, stdctx.CancelFunc) {
	timeout := defaultRequestTimeout
	if c != nil && c.Config != nil && c.Config.JenkinsRequestTimeoutSeconds > 0 {
		// The config stores a count of seconds; time.Duration's base unit is
		// nanoseconds, so multiply by time.Second to convert the unit.
		timeout = time.Duration(c.Config.JenkinsRequestTimeoutSeconds) * time.Second
	}
	return stdctx.WithTimeout(stdctx.Background(), timeout)
}

// DownloadContext bounds a streaming Jenkins download, which legitimately
// runs much longer than an ordinary API call. Callers own the CancelFunc.
func (c *ProgramContext) DownloadContext() (stdctx.Context, stdctx.CancelFunc) {
	return stdctx.WithTimeout(stdctx.Background(), defaultDownloadTimeout)
}
