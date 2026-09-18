//go:build heavy

package unicodenorm

// The Go statement of the browser's copy of the normalisation rule is
// unexported, so that no search path and no key path can call it. These names
// hand it to the external test package, which is its only user.

// ClientNormaliser is clientNormaliser, for the tests.
type ClientNormaliser = clientNormaliser

// NewClientNormaliser is newClientNormaliser, for the tests.
var NewClientNormaliser = newClientNormaliser

// ClientNFC is clientNFC, for the tests.
var ClientNFC = clientNFC

// AppendNFC is appendNFC, for the tests.
func (c *clientNormaliser) AppendNFC(dst, src []byte) []byte {
	return c.appendNFC(dst, src)
}
