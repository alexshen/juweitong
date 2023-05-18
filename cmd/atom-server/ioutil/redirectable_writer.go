package ioutil

import (
	"io"
	"sync/atomic"
)

// RedirectableWriter is a writer wrapping another underlying writer.
// You can change the underlying writer at runtime which is useful for logging.
type RedirectableWriter struct {
	w atomic.Value
}

// NewRedirectableWriter creates a RedirectableWriter with the underlying
// writer w
func NewRedirectableWriter(w io.Writer) *RedirectableWriter {
	rw := &RedirectableWriter{}
	rw.SetWriter(w)
	return rw
}

// SetWriter changes the underlying writer to the new writer
func (rw *RedirectableWriter) SetWriter(w io.Writer) {
	rw.w.Store(w)
}

// Write writes the byte slice using the underlying writer
func (rw *RedirectableWriter) Write(p []byte) (int, error) {
	w := rw.w.Load().(io.Writer)
	return w.Write(p)
}
