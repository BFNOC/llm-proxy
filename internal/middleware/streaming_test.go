package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStreamingMiddleware_NonStreamingRequest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("regular response")) //nolint:errcheck
	})

	streamingHandler := StreamingMiddleware()(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	recorder := httptest.NewRecorder()

	streamingHandler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", recorder.Code)
	}

	if recorder.Body.String() != "regular response" {
		t.Errorf("Expected body 'regular response', got '%s'", recorder.Body.String())
	}
}

func TestStreamingMiddleware_BasicHandlerExecution(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("chunk 1\n")) //nolint:errcheck
		w.Write([]byte("chunk 2\n")) //nolint:errcheck
		w.Write([]byte("chunk 3\n")) //nolint:errcheck
	})

	streamingHandler := StreamingMiddleware()(handler)

	req := httptest.NewRequest("POST", "/api/test", nil)
	recorder := httptest.NewRecorder()

	streamingHandler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", recorder.Code)
	}

	expectedBody := "chunk 1\nchunk 2\nchunk 3\n"
	if recorder.Body.String() != expectedBody {
		t.Errorf("Expected body '%s', got '%s'", expectedBody, recorder.Body.String())
	}
}

func TestStreamingResponseWriter_Write(t *testing.T) {
	recorder := &MockFlushableRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		flushCalled:      false,
	}

	streamingWriter := &streamingResponseWriter{
		ResponseWriter: recorder,
		flusher:        recorder,
	}

	testData := []byte("test streaming data")
	n, err := streamingWriter.Write(testData)

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if n != len(testData) {
		t.Errorf("Expected %d bytes written, got %d", len(testData), n)
	}

	if !recorder.flushCalled {
		t.Error("Expected flush to be called after write")
	}

	if recorder.Body.String() != string(testData) {
		t.Errorf("Expected body '%s', got '%s'", string(testData), recorder.Body.String())
	}
}

func TestStreamingResponseWriter_MultipleWrites(t *testing.T) {
	recorder := &MockFlushableRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		flushCallCount:   0,
	}

	streamingWriter := &streamingResponseWriter{
		ResponseWriter: recorder,
		flusher:        recorder,
	}

	data1 := []byte("first chunk")
	data2 := []byte(" second chunk")
	data3 := []byte(" third chunk")

	streamingWriter.Write(data1) //nolint:errcheck
	streamingWriter.Write(data2) //nolint:errcheck
	streamingWriter.Write(data3) //nolint:errcheck

	if recorder.flushCallCount != 3 {
		t.Errorf("Expected flush to be called 3 times, got %d", recorder.flushCallCount)
	}

	expectedData := "first chunk second chunk third chunk"
	if recorder.Body.String() != expectedData {
		t.Errorf("Expected body '%s', got '%s'", expectedData, recorder.Body.String())
	}
}

func TestStreamingMiddleware_HandlerPanic(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	streamingHandler := StreamingMiddleware()(handler)

	req := httptest.NewRequest("POST", "/panic", nil)
	recorder := httptest.NewRecorder()

	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()

		streamingHandler.ServeHTTP(recorder, req)
	}()

	if !panicked {
		t.Error("Expected handler to panic")
	}
}

// Mock flushable recorder for testing.
type MockFlushableRecorder struct {
	*httptest.ResponseRecorder
	flushCalled    bool
	flushCallCount int
}

func (m *MockFlushableRecorder) Flush() {
	m.flushCalled = true
	m.flushCallCount++
}
