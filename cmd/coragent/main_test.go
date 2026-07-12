package main

import (
	"testing"

	"github.com/blkcor/coragent/tui"
)

func TestVisualModeFromEnvironment(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want tui.VisualMode
	}{
		{name: "default", want: tui.TrueColorMode()},
		{name: "no color", env: map[string]string{"NO_COLOR": "1"}, want: tui.NoColorMode()},
		{name: "dumb terminal", env: map[string]string{"TERM": "dumb"}, want: tui.ASCIIMode()},
		{name: "explicit fallbacks", env: map[string]string{
			"CORAGENT_ASCII":          "yes",
			"CORAGENT_REDUCED_MOTION": "true",
		}, want: tui.VisualMode{Color: tui.ColorTrueColor, ASCII: true, ReducedMotion: true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lookup := func(name string) (string, bool) {
				value, ok := test.env[name]
				return value, ok
			}
			if got := visualModeFromEnvironment(lookup); got != test.want {
				t.Fatalf("visual mode = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestEnvironmentFlagUsesConservativeFalseValues(t *testing.T) {
	for _, value := range []string{"", "0", "false", "FALSE", "no", "off", " off "} {
		if enabledEnvironmentFlag(value) {
			t.Errorf("flag %q was enabled", value)
		}
	}
	for _, value := range []string{"1", "true", "yes", "on", "anything"} {
		if !enabledEnvironmentFlag(value) {
			t.Errorf("flag %q was disabled", value)
		}
	}
}
