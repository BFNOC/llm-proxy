package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSMiddleware_BasicHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response")) //nolint:errcheck
	})

	corsHandler := CORSMiddleware()(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	recorder := httptest.NewRecorder()

	corsHandler.ServeHTTP(recorder, req)

	if recorder.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("Expected Access-Control-Allow-Origin to be '*', got '%s'", recorder.Header().Get("Access-Control-Allow-Origin"))
	}

	expectedMethods := "GET, POST, PUT, DELETE, OPTIONS"
	if recorder.Header().Get("Access-Control-Allow-Methods") != expectedMethods {
		t.Errorf("Expected Access-Control-Allow-Methods to be '%s', got '%s'", expectedMethods, recorder.Header().Get("Access-Control-Allow-Methods"))
	}

	expectedHeaders := "Content-Type, Authorization, Accept, Cache-Control, x-api-key, anthropic-version"
	if recorder.Header().Get("Access-Control-Allow-Headers") != expectedHeaders {
		t.Errorf("Expected Access-Control-Allow-Headers to be '%s', got '%s'", expectedHeaders, recorder.Header().Get("Access-Control-Allow-Headers"))
	}

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", recorder.Code)
	}

	if recorder.Body.String() != "test response" {
		t.Errorf("Expected body 'test response', got '%s'", recorder.Body.String())
	}
}

func TestCORSMiddleware_OptionsRequest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for OPTIONS request")
	})

	corsHandler := CORSMiddleware()(handler)

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	recorder := httptest.NewRecorder()

	corsHandler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status 200 for OPTIONS, got %d", recorder.Code)
	}

	if recorder.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CORS headers should be set for OPTIONS request")
	}

	if recorder.Body.String() != "" {
		t.Errorf("Expected empty body for OPTIONS, got '%s'", recorder.Body.String())
	}
}

func TestCORSMiddleware_DifferentMethods(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success")) //nolint:errcheck
	})

	corsHandler := CORSMiddleware()(handler)

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/test", nil)
			recorder := httptest.NewRecorder()

			corsHandler.ServeHTTP(recorder, req)

			if recorder.Header().Get("Access-Control-Allow-Origin") != "*" {
				t.Errorf("CORS headers should be set for %s method", method)
			}

			if recorder.Code != http.StatusOK {
				t.Errorf("Expected status 200 for %s, got %d", method, recorder.Code)
			}
		})
	}
}

func TestCORSMiddleware_HandlerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error")) //nolint:errcheck
	})

	corsHandler := CORSMiddleware()(handler)

	req := httptest.NewRequest("POST", "/error", nil)
	recorder := httptest.NewRecorder()

	corsHandler.ServeHTTP(recorder, req)

	if recorder.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CORS headers should be set even on error responses")
	}

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", recorder.Code)
	}

	if recorder.Body.String() != "internal error" {
		t.Errorf("Expected body 'internal error', got '%s'", recorder.Body.String())
	}
}
