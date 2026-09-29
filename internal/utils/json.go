package utils

import (
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"os"
)

// printJSONOptions are the options every command result is encoded with: the
// options of encoding/json's own Encoder (json.DefaultOptionsV1, which also
// selects its error values), HTML escaping off, and the two-space indentation
// applied by the encoder itself as it writes, so the output is produced in ONE
// pass and no second pass re-indents bytes already encoded (SPEC/IMPLEMENTATION.md
// § Performance Considerations, item 6).
//
// The bytes are those json.Encoder with SetEscapeHTML(false) and SetIndent("",
// "  ") writes, whose indentation step is jsontext's formatter with these same
// indentation options: two-space indentation and no prefix; `<`, `>` and `&`
// written literally; U+2028 and U+2029 escaped; an invalid UTF-8 byte in a string
// replaced by U+FFFD; an empty array written as `[]`. PrintJSON adds the one
// trailing newline. TestPrintJSON_ByteIdenticalToIndentingEncoder holds the two
// equal.
var printJSONOptions = jsonv2.JoinOptions(
	json.DefaultOptionsV1(),
	jsontext.EscapeForHTML(false),
	jsontext.Multiline(true),
	jsontext.WithIndentPrefix(""),
	jsontext.WithIndent("  "),
)

// PrintJSON outputs a value as human-readable indented JSON to stdout, with
// 2-space indentation and one trailing newline. Nothing is written when the value
// cannot be encoded.
//
// SECURITY NOTE: HTML escaping is intentionally disabled because:
// 1. This is a CLI application, not a web service
// 2. Output goes to stdout for consumption by other CLI tools or scripts
// 3. HTML escaping would make output harder to parse (e.g., "<" becomes "\u003c")
// 4. No web browser is involved in rendering this output
// If this output were to be used in a web context, HTML escaping should be enabled.
func PrintJSON(v any) error {
	return writeJSON(os.Stdout, v)
}

// writeJSON is PrintJSON writing to w: the value is encoded in full, in one pass,
// before a single write of the encoded bytes and the trailing newline.
func writeJSON(w io.Writer, v any) error {
	out, err := jsonv2.Marshal(v, printJSONOptions)
	if err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	out = append(out, '\n')
	if _, err := w.Write(out); err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	return nil
}

// ToJSON converts a value to a JSON byte slice.
func ToJSON(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshaling JSON: %w", err)
	}
	return data, nil
}

// FromJSON parses JSON data into a value.
func FromJSON(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unmarshaling JSON: %w", err)
	}
	return nil
}
