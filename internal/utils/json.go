package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// encoderConfig holds the encoder configuration to avoid repeated setup.
// Note: We don't cache the encoder itself because it captures os.Stdout at creation time,
// which causes issues when stdout is redirected (e.g., in tests).
// Instead, we cache the configuration and apply it to new encoders.
var encoderConfig = struct {
	once sync.Once
	// Pre-computed indent prefix and value for indented output
	indentPrefix string
	indentValue  string
}{}

// initEncoderConfig initializes the encoder configuration once.
func initEncoderConfig() {
	encoderConfig.once.Do(func() {
		encoderConfig.indentPrefix = ""
		encoderConfig.indentValue = "  "
	})
}

// PrintJSON outputs a value as JSON to stdout.
// Uses compact format (no pretty-print) for efficient parsing.
//
// SECURITY NOTE: SetEscapeHTML(false) is intentionally disabled because:
// 1. This is a CLI application, not a web service
// 2. Output goes to stdout for consumption by other CLI tools or scripts
// 3. HTML escaping would make output harder to parse (e.g., "<" becomes "\u003c")
// 4. No web browser is involved in rendering this output
// If this output were to be used in a web context, HTML escaping should be enabled.
func PrintJSON(v interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	return nil
}

// PrintJSONIndent outputs a value as indented JSON to stdout.
// Useful for debugging or human-readable output.
//
// SECURITY NOTE: SetEscapeHTML(false) is intentionally disabled - see PrintJSON for details.
func PrintJSONIndent(v interface{}) error {
	initEncoderConfig()
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent(encoderConfig.indentPrefix, encoderConfig.indentValue)
	if err := encoder.Encode(v); err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	return nil
}

// ToJSON converts a value to a JSON byte slice.
func ToJSON(v interface{}) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshaling JSON: %w", err)
	}
	return data, nil
}

// FromJSON parses JSON data into a value.
func FromJSON(data []byte, v interface{}) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unmarshaling JSON: %w", err)
	}
	return nil
}
