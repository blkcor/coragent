package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const DefaultQueueSize = 10

// Config is Mercury's public runtime configuration.
type Config struct {
	QueueSize  int      `json:"queue_size"`
	Retries    int      `json:"retries"`
	Extensions []string `json:"extensions"`
}

type layer struct {
	QueueSize  *int      `json:"queue_size,omitempty"`
	Retries    *int      `json:"retries,omitempty"`
	Extensions *[]string `json:"extensions,omitempty"`
}

// FlagValues contains only explicitly supplied command-line values.
type FlagValues struct {
	QueueSize *int
	Retries   *int
}

func Defaults() Config {
	return Config{QueueSize: DefaultQueueSize, Retries: 3, Extensions: []string{".go", ".json"}}
}

// Load applies layers from lowest to highest precedence: defaults, user file,
// project file, MERCURY_* environment, then command-line flags. Validation is
// deliberately separate and runs only after all loading is complete.
func Load(userPath, projectPath string, environ map[string]string, flags FlagValues) (Config, error) {
	cfg := Defaults()
	user, err := loadFile(userPath)
	if err != nil {
		return Config{}, fmt.Errorf("load user configuration: %w", err)
	}
	apply(&cfg, user)
	project, err := loadFile(projectPath)
	if err != nil {
		return Config{}, fmt.Errorf("load project configuration: %w", err)
	}
	apply(&cfg, project)
	if value, ok := environ["MERCURY_QUEUE_SIZE"]; ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse MERCURY_QUEUE_SIZE: %w", err)
		}
		cfg.QueueSize = parsed
	}
	if value, ok := environ["MERCURY_RETRIES"]; ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse MERCURY_RETRIES: %w", err)
		}
		cfg.Retries = parsed
	}
	if flags.QueueSize != nil {
		cfg.QueueSize = *flags.QueueSize
	}
	if flags.Retries != nil {
		cfg.Retries = *flags.Retries
	}
	return cfg, nil
}

func Validate(cfg Config) error {
	if cfg.QueueSize <= 0 {
		return errors.New("queue_size must be positive")
	}
	if cfg.Retries < 0 {
		return errors.New("retries must not be negative")
	}
	if len(cfg.Extensions) == 0 {
		return errors.New("extensions must not be empty")
	}
	for _, extension := range cfg.Extensions {
		if !strings.HasPrefix(extension, ".") {
			return fmt.Errorf("extension %q must start with a dot", extension)
		}
	}
	return nil
}

func loadFile(name string) (layer, error) {
	if name == "" {
		return layer{}, nil
	}
	data, err := os.ReadFile(name)
	if errors.Is(err, os.ErrNotExist) {
		return layer{}, nil
	}
	if err != nil {
		return layer{}, err
	}
	var value layer
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return layer{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return layer{}, errors.New("trailing configuration data")
	}
	return value, nil
}

func apply(cfg *Config, value layer) {
	if value.QueueSize != nil {
		cfg.QueueSize = *value.QueueSize
	}
	if value.Retries != nil {
		cfg.Retries = *value.Retries
	}
	if value.Extensions != nil {
		cfg.Extensions = append([]string(nil), (*value.Extensions)...)
	}
}
