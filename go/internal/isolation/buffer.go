package isolation

import "sync"

type boundedBuffer struct {
	mu        sync.Mutex
	maximum   int64
	data      []byte
	truncated bool
}

func newBoundedBuffer(maximum int64) *boundedBuffer {
	return &boundedBuffer{maximum: maximum}
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	available := buffer.maximum - int64(len(buffer.data))
	if available > 0 {
		length := int64(len(data))
		if length > available {
			length = available
		}
		buffer.data = append(buffer.data, data[:int(length)]...)
	}
	if int64(len(data)) > available {
		buffer.truncated = true
	}
	return len(data), nil
}

func (buffer *boundedBuffer) snapshot() ([]byte, bool) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return append([]byte(nil), buffer.data...), buffer.truncated
}
