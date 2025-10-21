package a2a

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func TestA2AHandlerMux_Deadlock(t *testing.T) {
	// Create a new A2AHandlerMux
	muxer := NewA2AHttpMux("/api/a2a", nil)
	handlerName := "default/test-agent"

	// Create a handler that will try to get a write lock on the muxer
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This handler will try to get a write lock on the muxer, which would cause a deadlock
		// if the muxer's ServeHTTP method holds a read lock while calling the handler.
		muxer.RemoveAgentHandler(handlerName)
		w.WriteHeader(http.StatusOK)
	})

	// Add the handler to the muxer
	muxer.AddAgentHandler(handlerName, handler)

	// Create a new request
	req := httptest.NewRequest("GET", "/api/a2a/default/test-agent/", nil)
	req = mux.SetURLVars(req, map[string]string{"namespace": "default", "name": "test-agent"})

	// Create a response recorder
	rr := httptest.NewRecorder()

	// Use a channel to signal when the request is done
	done := make(chan struct{})

	// Run the ServeHTTP method in a separate goroutine
	go func() {
		muxer.ServeHTTP(rr, req)
		close(done)
	}()

	// Wait for the request to complete, with a timeout to detect deadlocks
	select {
	case <-done:
		// The request completed without a deadlock
		require.Equal(t, http.StatusOK, rr.Code)
	case <-time.After(1 * time.Second):
		// The request timed out, which indicates a deadlock
		t.Fatal("deadlock detected")
	}
}
