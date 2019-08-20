package cypress

// DummyWriter drops all data to be written
type DummyWriter struct{}

// Write drops the data
func (w *DummyWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// BufferedWriter writes everything into memory
type BufferedWriter struct {
	Buffer [][]byte
}

// NewBufferWriter creates a new buffer based writer
func NewBufferWriter() *BufferedWriter {
	return &BufferedWriter{make([][]byte, 0)}
}

// Write save the data into buffer
func (w *BufferedWriter) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	buf := make([]byte, len(p))
	copy(buf, p)
	w.Buffer = append(w.Buffer, buf)
	return len(p), nil
}
