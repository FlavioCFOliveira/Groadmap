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
// into memory, and only then are the encoded bytes and the trailing newline
// written, so a value that cannot be encoded writes nothing.
//
// The encoded bytes are held in fixed-size chunks rather than in one growing
// slice: a large result (several megabytes for a sprint of thousands of tasks)
// is then never reallocated and copied as it grows, and each chunk is written in
// turn once the encoding has succeeded.
func writeJSON(w io.Writer, v any) error {
	var out chunkedBuffer
	if err := jsonv2.MarshalWrite(&out, v, printJSONOptions); err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	out.writeByte('\n')
	if err := out.writeTo(w); err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	return nil
}

// jsonChunkSize is the capacity of each chunk of a chunkedBuffer.
const jsonChunkSize = 64 << 10

// chunkedBuffer is an in-memory io.Writer that stores what it is given in
// chunks of jsonChunkSize bytes. A chunk, once allocated, is never reallocated,
// copied, or moved, so appending costs one copy of the written bytes whatever
// the total size.
type chunkedBuffer struct {
	chunks [][]byte
}

// Write appends p to the buffer. It never fails.
func (b *chunkedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		last := b.tail()
		k := min(cap(*last)-len(*last), len(p))
		*last = append(*last, p[:k]...)
		p = p[k:]
	}
	return n, nil
}

// writeByte appends one byte to the buffer.
func (b *chunkedBuffer) writeByte(c byte) {
	last := b.tail()
	*last = append(*last, c)
}

// tail returns the last chunk, allocating a new one first when there is none or
// when the last one is full.
func (b *chunkedBuffer) tail() *[]byte {
	if n := len(b.chunks); n == 0 || len(b.chunks[n-1]) == cap(b.chunks[n-1]) {
		b.chunks = append(b.chunks, make([]byte, 0, jsonChunkSize))
	}
	return &b.chunks[len(b.chunks)-1]
}

// writeTo writes every chunk to w, in order, and stops at the first error.
func (b *chunkedBuffer) writeTo(w io.Writer) error {
	for _, chunk := range b.chunks {
		if _, err := w.Write(chunk); err != nil {
			return err
		}
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
