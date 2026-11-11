package engine

import (
	"fmt"
	"io"
	"os"

	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/host"
)

type ResolutionSource string

const (
	SourceFlag            ResolutionSource = "flag"
	SourceEnv             ResolutionSource = "env"
	SourceHostDetection   ResolutionSource = "host_detection"
	SourceConfig          ResolutionSource = "config"
	SourceFallbackDefault ResolutionSource = "fallback_default"
)

type EngineResolution struct {
	EngineName string           `json:"engine_name"`
	Source     ResolutionSource `json:"source"`
	HostInfo   host.HostInfo    `json:"host_info"`
}

// WarnUnknownHost outputs a diagnostic notice to w if host detection was unknown and
// engine resolution fell back to config or fallback default.
func (r EngineResolution) WarnUnknownHost(w io.Writer) {
	if w == nil {
		return
	}
	if r.HostInfo.Kind == host.HostUnknown && (r.Source == SourceConfig || r.Source == SourceFallbackDefault) {
		fmt.Fprintf(w, "[buddy] host environment unknown (neither Rex nor T3 detected); defaulting to engine '%s'. Override with --engine <name> or BUDDY_ENGINE.\n", r.EngineName)
	}
}

// ResolveEngine implements the 5-tier adaptive engine selection precedence:
// 1. Explicit CLI flag (--engine)
// 2. Environment override (BUDDY_ENGINE, then BUDDY_WORKER_BACKEND)
// 3. Host environment detection (Rex -> rex, T3 -> t3)
// 4. User configuration (cfg.DefaultEngine)
// 5. Fallback default ("rex")
func ResolveEngine(flagEngine string, getEnv func(string) string, cfg *config.Config) EngineResolution {
	if getEnv == nil {
		getEnv = os.Getenv
	}

	hostInfo := host.DetectHostFromEnv(getEnv)

	// Tier 1: Explicit CLI flag
	if flagEngine != "" {
		return EngineResolution{
			EngineName: flagEngine,
			Source:     SourceFlag,
			HostInfo:   hostInfo,
		}
	}

	// Tier 2: Environment override
	if env := getEnv("BUDDY_ENGINE"); env != "" {
		return EngineResolution{
			EngineName: env,
			Source:     SourceEnv,
			HostInfo:   hostInfo,
		}
	}
	if env := getEnv("BUDDY_WORKER_BACKEND"); env != "" {
		return EngineResolution{
			EngineName: env,
			Source:     SourceEnv,
			HostInfo:   hostInfo,
		}
	}

	// Tier 3: Host detection
	switch hostInfo.Kind {
	case host.HostRex:
		return EngineResolution{
			EngineName: "rex",
			Source:     SourceHostDetection,
			HostInfo:   hostInfo,
		}
	case host.HostT3:
		return EngineResolution{
			EngineName: "t3",
			Source:     SourceHostDetection,
			HostInfo:   hostInfo,
		}
	}

	// Tier 4: User configuration
	if cfg != nil && cfg.DefaultEngine != "" {
		return EngineResolution{
			EngineName: cfg.DefaultEngine,
			Source:     SourceConfig,
			HostInfo:   hostInfo,
		}
	}

	// Tier 5: Fallback default
	return EngineResolution{
		EngineName: "rex",
		Source:     SourceFallbackDefault,
		HostInfo:   hostInfo,
	}
}
