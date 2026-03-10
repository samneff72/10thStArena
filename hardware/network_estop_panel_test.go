package hardware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNetworkEStopPanelSuccessfulPoll(t *testing.T) {
	want := []EStopEvent{
		{Station: "R1", IsAStop: false},
		{Station: "B2", IsAStop: true},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/poll", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL)
	got := panel.Poll()
	assert.Equal(t, want, got)
}

func TestNetworkEStopPanelEmptyPoll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL)
	got := panel.Poll()
	assert.Empty(t, got)
}

func TestNetworkEStopPanelConnectionFailure(t *testing.T) {
	panel := NewNetworkEStopPanel("http://127.0.0.1:1") // nothing listening
	got := panel.Poll()
	assert.Nil(t, got)
}

func TestNetworkEStopPanelTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond) // longer than the 200 ms client timeout
		w.Write([]byte("[]"))
	}))
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL)
	start := time.Now()
	got := panel.Poll()
	elapsed := time.Since(start)
	assert.Nil(t, got)
	assert.Less(t, elapsed, 400*time.Millisecond)
}

func TestNetworkEStopPanelNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	panel := NewNetworkEStopPanel(srv.URL)
	got := panel.Poll()
	assert.Nil(t, got)
}

func TestNetworkEStopPanelBareHostNormalised(t *testing.T) {
	want := []EStopEvent{{Station: "all", IsAStop: false}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	// Strip the "http://" scheme to test bare-host normalisation.
	bare := strings.TrimPrefix(srv.URL, "http://")
	panel := NewNetworkEStopPanel(bare)
	got := panel.Poll()
	assert.Equal(t, want, got)
}
