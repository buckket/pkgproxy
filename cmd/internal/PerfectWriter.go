package internal

import "io"

// PerfectWriter never fails a write... if one fails, it lies and returns
// no error, while refusing to write further data. This is used with
// io.MultiWriter so that errors some Writers can be ignored.
// The first write error is made available via Error().
type PerfectWriter struct {
	writer io.Writer
	err    error
}

// NewPerfectWriter wraps a writer into a PerfectWriter and returns it
func NewPerfectWriter(w io.Writer) *PerfectWriter {
	return &PerfectWriter{w, nil}
}

func (w *PerfectWriter) Error() error {
	return w.err
}

func (w *PerfectWriter) Write(data []byte) (int, error) {
	if w.err == nil {
		_, w.err = w.writer.Write(data)
	}
	return len(data), nil
}
