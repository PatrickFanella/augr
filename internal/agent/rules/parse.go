package rules

import (
	"encoding/json"
	"fmt"
)

// Parse decodes and validates a RulesEngineConfig from raw JSON.
func Parse(raw json.RawMessage) (*RulesEngineConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var cfg RulesEngineConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("rules: invalid JSON: %w", err)
	}
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
