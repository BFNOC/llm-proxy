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

func TestResponseStatusCapture_ImplementsFlusher(t *testing.T) {
	capture := &responseStatusCapture{
		ResponseWriter: httptest.NewRecorder(),
		statusCode:     http.StatusOK,
	}

	if _, ok := interface{}(capture).(http.Flusher); !ok {
		t.Fatal("responseStatusCapture must expose http.Flusher for streaming middleware")
	}
}

func TestResponseStatusCapture_FlushDelegatesToUnderlying(t *testing.T) {
	recorder := &MockFlushableRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}
	capture := &responseStatusCapture{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	capture.Flush()
	capture.Flush()

	if recorder.flushCallCount != 2 {
		t.Errorf("Expected 2 flush calls on underlying writer, got %d", recorder.flushCallCount)
	}
}

func TestAuditCaptureThenStreaming_FlushesEachWrite(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: chunk1\n\n")) //nolint:errcheck
		w.Write([]byte("data: chunk2\n\n")) //nolint:errcheck
		w.Write([]byte("data: chunk3\n\n")) //nolint:errcheck
	})

	// 真实链路中 AuditLogMiddleware 位于 StreamingMiddleware 外层。
	// 这里直接用 responseStatusCapture 包住 recorder，验证 StreamingMiddleware
	// 能识别到被审计包装后的 Flusher 能力，并逐块刷新 SSE。
	streamingHandler := StreamingMiddleware()(handler)
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	recorder := &MockFlushableRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}
	capture := &responseStatusCapture{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	streamingHandler.ServeHTTP(capture, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", recorder.Code)
	}
	if recorder.flushCallCount < 3 {
		t.Errorf("Expected at least 3 flush calls, got %d", recorder.flushCallCount)
	}
	expectedBody := "data: chunk1\n\ndata: chunk2\n\ndata: chunk3\n\n"
	if recorder.Body.String() != expectedBody {
		t.Errorf("Expected body %q, got %q", expectedBody, recorder.Body.String())
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
