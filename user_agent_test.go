package client

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestDefaultUserAgent(t *testing.T) {
	t.Parallel()

	if ua := defaultUserAgent(); !strings.HasPrefix(ua, "slack-manager-go-client/") || len(ua) == len("slack-manager-go-client/") {
		t.Errorf("unexpected user agent %q", ua)
	}
}

func TestModuleVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		info     *debug.BuildInfo
		expected string
	}{
		{"no build info", nil, "dev"},
		{"main module devel", &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "(devel)"}}, "dev"},
		{"main module tagged", &debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "v0.5.0"}}, "v0.5.0"},
		{"dependency", &debug.BuildInfo{
			Main: debug.Module{Path: "example.com/app"},
			Deps: []*debug.Module{{Path: "example.com/other", Version: "v1.0.0"}, {Path: modulePath, Version: "v0.5.0"}},
		}, "v0.5.0"},
		{"replaced dependency", &debug.BuildInfo{
			Main: debug.Module{Path: "example.com/app"},
			Deps: []*debug.Module{{Path: modulePath, Version: "v0.5.0", Replace: &debug.Module{Path: "../go-client"}}},
		}, "dev"},
		{"not a dependency", &debug.BuildInfo{Main: debug.Module{Path: "example.com/app"}}, "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := moduleVersion(tt.info); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}
