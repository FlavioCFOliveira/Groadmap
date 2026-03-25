package utils

import (
	"encoding/json"
	"fmt"
	"os"
)

// PrintJSON outputs a value as human-readable indented JSON to stdout.
// Uses 2-space indentation for readability.
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
	encoder.SetIndent("", "  ")
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
