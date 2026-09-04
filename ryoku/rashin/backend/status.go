package main

import "time"

// Status is the full daemon report for `status --json`, the Hub page, and
// /api/status.
type Status struct {
	Enabled bool        `json:"enabled"`
	Running bool        `json:"running"`
	Port    int         `json:"port"`
	Vault   VaultStatus `json:"vault"`
	Hermes  HermesInfo  `json:"hermes"`
	Agents  []Agent     `json:"agents"`
}

// VaultStatus is the vault summary embedded in Status.
type VaultStatus struct {
	Path        string    `json:"path"`
	Exists      bool      `json:"exists"`
	Files       int       `json:"files"`
	LastIndexed time.Time `json:"lastIndexed"`
}

// BuildStatus assembles the report. Running is probed over the loopback API
// (pingDaemon, in server.go) so the answer reflects a live daemon, not just the
// enabled gate.
func BuildStatus(cfg Config) Status {
	files, last, exists := VaultStats()
	return Status{
		Enabled: cfg.Enabled,
		Running: pingDaemon(cfg.Port),
		Port:    cfg.Port,
		Vault: VaultStatus{
			Path:        VaultDir(),
			Exists:      exists,
			Files:       files,
			LastIndexed: last,
		},
		Hermes: HermesStatus(),
		Agents: DetectAgents(),
	}
}
