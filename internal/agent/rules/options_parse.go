package rules

import (
	"encoding/json"
	"fmt"
)

// ParseOptions decodes and validates an OptionsRulesConfig from raw JSON.
func ParseOptions(raw json.RawMessage) (*OptionsRulesConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var cfg OptionsRulesConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("rules: invalid options JSON: %w", err)
	}
	if err := ValidateOptions(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
