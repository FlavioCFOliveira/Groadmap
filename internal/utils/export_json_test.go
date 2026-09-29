package utils

// WriteJSON exposes PrintJSON's writer form to the external test package, so the
// golden test captures the exact bytes without redirecting the process's stdout.
var WriteJSON = writeJSON
