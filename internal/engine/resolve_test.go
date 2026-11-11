package engine

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tortoise10h/buddy/internal/config"
	"github.com/tortoise10h/buddy/internal/host"
)

func mockEnv(m map[string]string) func(string) string {
	return func(key string) string {
		return m[key]
	}
}

func TestResolveEnginePrecedence(t *testing.T) {
	tests := []struct {
		name       string
		flag       string
		env        map[string]string
		cfg        *config.Config
		wantEngine string
		wantSource ResolutionSource
		wantHost   host.HostKind
	}{
		{
			name:       "Tier 1: flag beats env, host, config, and default",
			flag:       "custom-eng",
			env:        map[string]string{"BUDDY_ENGINE": "env-eng", "T3CODE_THREAD_ID": "th123"},
			cfg:        &config.Config{DefaultEngine: "cfg-eng"},
			wantEngine: "custom-eng",
			wantSource: SourceFlag,
			wantHost:   host.HostT3,
		},
		{
			name:       "Tier 2: BUDDY_ENGINE beats host, config, and default",
			flag:       "",
			env:        map[string]string{"BUDDY_ENGINE": "override-eng", "REX_SESSION": "sess123"},
			cfg:        &config.Config{DefaultEngine: "rex"},
			wantEngine: "override-eng",
			wantSource: SourceEnv,
			wantHost:   host.HostRex,
		},
		{
			name:       "Tier 2: BUDDY_WORKER_BACKEND secondary env override beats host and config",
			flag:       "",
			env:        map[string]string{"BUDDY_WORKER_BACKEND": "backend-eng", "T3_THREAD_ID": "th456"},
			cfg:        &config.Config{DefaultEngine: "rex"},
			wantEngine: "backend-eng",
			wantSource: SourceEnv,
			wantHost:   host.HostT3,
		},
		{
			name: "Tier 2: BUDDY_ENGINE beats BUDDY_WORKER_BACKEND",
			flag: "",
			env: map[string]string{
				"BUDDY_ENGINE":         "first-env",
				"BUDDY_WORKER_BACKEND": "second-env",
			},
			cfg:        &config.Config{DefaultEngine: "rex"},
			wantEngine: "first-env",
			wantSource: SourceEnv,
			wantHost:   host.HostUnknown,
		},
		{
			name:       "Tier 3: host T3 beats cfg.DefaultEngine='rex'",
			flag:       "",
			env:        map[string]string{"T3CODE_THREAD_ID": "th_inner"},
			cfg:        &config.Config{DefaultEngine: "rex"},
			wantEngine: "t3",
			wantSource: SourceHostDetection,
			wantHost:   host.HostT3,
		},
		{
			name:       "Tier 3: host Rex beats cfg.DefaultEngine='custom'",
			flag:       "",
			env:        map[string]string{"REX_SESSION": "session:abc"},
			cfg:        &config.Config{DefaultEngine: "custom"},
			wantEngine: "rex",
			wantSource: SourceHostDetection,
			wantHost:   host.HostRex,
		},
		{
			name: "Tier 3: host conflict Rex+T3 resolves to t3 and beats config",
			flag: "",
			env: map[string]string{
				"REX_SESSION":      "session:abc",
				"T3CODE_THREAD_ID": "th_inner",
			},
			cfg:        &config.Config{DefaultEngine: "rex"},
			wantEngine: "t3",
			wantSource: SourceHostDetection,
			wantHost:   host.HostT3,
		},
		{
			name:       "Tier 4: unknown host falls back to cfg.DefaultEngine",
			flag:       "",
			env:        map[string]string{},
			cfg:        &config.Config{DefaultEngine: "my-engine"},
			wantEngine: "my-engine",
			wantSource: SourceConfig,
			wantHost:   host.HostUnknown,
		},
		{
			name:       "Tier 5: unknown host with empty config falls back to default rex",
			flag:       "",
			env:        map[string]string{},
			cfg:        nil,
			wantEngine: "rex",
			wantSource: SourceFallbackDefault,
			wantHost:   host.HostUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ResolveEngine(tt.flag, mockEnv(tt.env), tt.cfg)
			if res.EngineName != tt.wantEngine {
				t.Errorf("ResolveEngine() EngineName = %q, want %q", res.EngineName, tt.wantEngine)
			}
			if res.Source != tt.wantSource {
				t.Errorf("ResolveEngine() Source = %v, want %v", res.Source, tt.wantSource)
			}
			if res.HostInfo.Kind != tt.wantHost {
				t.Errorf("ResolveEngine() HostInfo.Kind = %v, want %v", res.HostInfo.Kind, tt.wantHost)
			}
		})
	}
}

func TestWarnUnknownHost(t *testing.T) {
	// Case 1: Unknown host fallback to config
	res1 := EngineResolution{
		EngineName: "custom",
		Source:     SourceConfig,
		HostInfo: host.HostInfo{
			Kind: host.HostUnknown,
		},
	}
	var buf1 bytes.Buffer
	res1.WarnUnknownHost(&buf1)
	wantMsg1 := "[buddy] host environment unknown (neither Rex nor T3 detected); defaulting to engine 'custom'. Override with --engine <name> or BUDDY_ENGINE.\n"
	if buf1.String() != wantMsg1 {
		t.Errorf("WarnUnknownHost got %q, want %q", buf1.String(), wantMsg1)
	}

	// Case 2: Unknown host fallback to default rex
	res2 := EngineResolution{
		EngineName: "rex",
		Source:     SourceFallbackDefault,
		HostInfo: host.HostInfo{
			Kind: host.HostUnknown,
		},
	}
	var buf2 bytes.Buffer
	res2.WarnUnknownHost(&buf2)
	wantMsg2 := "[buddy] host environment unknown (neither Rex nor T3 detected); defaulting to engine 'rex'. Override with --engine <name> or BUDDY_ENGINE.\n"
	if buf2.String() != wantMsg2 {
		t.Errorf("WarnUnknownHost got %q, want %q", buf2.String(), wantMsg2)
	}

	// Case 3: Flag override - no warning
	res3 := EngineResolution{
		EngineName: "rex",
		Source:     SourceFlag,
		HostInfo: host.HostInfo{
			Kind: host.HostUnknown,
		},
	}
	var buf3 bytes.Buffer
	res3.WarnUnknownHost(&buf3)
	if buf3.Len() != 0 {
		t.Errorf("WarnUnknownHost should not warn on flag override, got %q", buf3.String())
	}

	// Case 4: Known host (Rex) - no warning
	res4 := EngineResolution{
		EngineName: "rex",
		Source:     SourceHostDetection,
		HostInfo: host.HostInfo{
			Kind: host.HostRex,
		},
	}
	var buf4 bytes.Buffer
	res4.WarnUnknownHost(&buf4)
	if buf4.Len() != 0 {
		t.Errorf("WarnUnknownHost should not warn on known host, got %q", buf4.String())
	}
}

func TestNilEnvAndConfig(t *testing.T) {
	res := ResolveEngine("", nil, nil)
	if res.EngineName == "" {
		t.Errorf("expected non-empty engine name with nil env and config")
	}
	if !strings.Contains(string(res.Source), "host_detection") && res.Source != SourceFallbackDefault {
		t.Errorf("unexpected resolution source: %s", res.Source)
	}
}
