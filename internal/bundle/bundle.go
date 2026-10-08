package bundle

import (
	"encoding/json"
	"fmt"
	"os"
)

const (
	ClassOOM     = "container_oom"
	ClassNetwork = "network_timeout"
	ClassConfig  = "config_regression"
)

// Bundle is captured production evidence for one supported incident.
// It is not a recording. Runners rebuild only the conditions named here.
type Bundle struct {
	ID       string            `json:"id"`
	Class    string            `json:"class"`
	Service  string            `json:"service"`
	Symptom  string            `json:"symptom"`
	Evidence Evidence          `json:"evidence"`
	Workload Workload          `json:"workload"`
	Network  *NetworkCondition `json:"network,omitempty"`
	Configs  *ConfigPair       `json:"configs,omitempty"`
}

type Evidence struct {
	MemoryLimitBytes  int64    `json:"memory_limit_bytes,omitempty"`
	ObservedPeakBytes int64    `json:"observed_peak_bytes,omitempty"`
	RestartCount      int      `json:"restart_count,omitempty"`
	P99Ms             int      `json:"p99_ms,omitempty"`
	DeployChange      string   `json:"deploy_change,omitempty"`
	Notes             []string `json:"notes,omitempty"`
}

type Workload struct {
	Name             string `json:"name"`
	Requests         int    `json:"requests"`
	BytesPerRequest  int64  `json:"bytes_per_request"`
	Hold             bool   `json:"hold"`
	ExpectedMaxP99Ms int    `json:"expected_max_p99_ms"`
}

type NetworkCondition struct {
	DelayMs     int  `json:"delay_ms"`
	LossPercent int  `json:"loss_percent"`
	TimeoutMs   int  `json:"timeout_ms"`
	PolicyDeny  bool `json:"policy_deny"`
	Attempts    int  `json:"attempts"`
}

type ConfigPair struct {
	Previous map[string]string `json:"previous"`
	Current  map[string]string `json:"current"`
}

func Load(path string) (Bundle, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Bundle{}, err
	}
	return Parse(raw)
}

func Parse(raw []byte) (Bundle, error) {
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return Bundle{}, err
	}
	if err := b.Validate(); err != nil {
		return Bundle{}, err
	}
	return b, nil
}

func (b Bundle) Validate() error {
	if b.ID == "" {
		return fmt.Errorf("bundle id is required")
	}
	switch b.Class {
	case ClassOOM, ClassNetwork, ClassConfig:
	default:
		return fmt.Errorf("unsupported class %q", b.Class)
	}
	if b.Class == ClassOOM {
		if b.Workload.Requests < 1 {
			return fmt.Errorf("oom workload requests must be >= 1")
		}
		if b.Workload.BytesPerRequest < 1 {
			return fmt.Errorf("oom workload bytes_per_request must be >= 1")
		}
		if b.Evidence.MemoryLimitBytes < 1 {
			return fmt.Errorf("oom evidence memory_limit_bytes must be >= 1")
		}
	}
	if b.Class == ClassNetwork {
		if b.Network == nil {
			return fmt.Errorf("network bundle requires network conditions")
		}
		if b.Network.Attempts < 1 {
			return fmt.Errorf("network attempts must be >= 1")
		}
		if b.Network.TimeoutMs < 1 {
			return fmt.Errorf("network timeout_ms must be >= 1")
		}
	}
	if b.Class == ClassConfig {
		if b.Configs == nil || len(b.Configs.Previous) == 0 || len(b.Configs.Current) == 0 {
			return fmt.Errorf("config bundle requires previous and current configs")
		}
	}
	return nil
}

func Diff(prev, curr map[string]string) map[string][2]string {
	out := map[string][2]string{}
	keys := map[string]struct{}{}
	for k := range prev {
		keys[k] = struct{}{}
	}
	for k := range curr {
		keys[k] = struct{}{}
	}
	for k := range keys {
		if prev[k] != curr[k] {
			out[k] = [2]string{prev[k], curr[k]}
		}
	}
	return out
}
