package credential

import (
	"context"
	"errors"
	"testing"
)

func TestStaticAndMissingEnvironment(t *testing.T) {
	got, err := (Static{Value: "runtime-only"}).Credential(context.Background())
	if err != nil || got != "runtime-only" {
		t.Fatalf("Static = %q, %v", got, err)
	}
	t.Setenv("CORAGENT_TEST_MISSING_KEY", "")
	_, err = (EnvSource{Name: "CORAGENT_TEST_MISSING_KEY"}).Credential(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("EnvSource = %v", err)
	}
}
