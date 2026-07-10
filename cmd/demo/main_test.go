package main

import (
	"reflect"
	"testing"

	"github.com/blkcor/coragent/internal/config"
	"github.com/blkcor/coragent/pkg/agent"
)

func TestApplySandboxSettings(t *testing.T) {
	network := true
	settings := config.Settings{Sandbox: &config.SandboxSettings{
		ExtraReadRoots:  []string{"/read"},
		ExtraWriteRoots: []string{"/write"},
		Network:         &network,
	}}
	var sc agent.SessionConfig

	applySandboxSettings(&sc, settings)

	if !reflect.DeepEqual(sc.SandboxExtraReadRoots, []string{"/read"}) {
		t.Fatalf("read roots = %v", sc.SandboxExtraReadRoots)
	}
	if !reflect.DeepEqual(sc.SandboxExtraWriteRoots, []string{"/write"}) {
		t.Fatalf("write roots = %v", sc.SandboxExtraWriteRoots)
	}
	if !sc.SandboxNetwork {
		t.Fatalf("network grant should be copied")
	}

	settings.Sandbox.ExtraReadRoots[0] = "/mutated"
	settings.Sandbox.ExtraWriteRoots[0] = "/mutated"
	if sc.SandboxExtraReadRoots[0] != "/read" || sc.SandboxExtraWriteRoots[0] != "/write" {
		t.Fatalf("session config should own copied sandbox roots")
	}
}

func TestApplySandboxSettingsLeavesNetworkDefaultWhenUnset(t *testing.T) {
	sc := agent.SessionConfig{SandboxNetwork: true}

	applySandboxSettings(&sc, config.Settings{Sandbox: &config.SandboxSettings{}})

	if !sc.SandboxNetwork {
		t.Fatalf("unspecified network should not override existing session config")
	}
}
