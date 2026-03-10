package hardware

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

// NetworkEStopPanel implements EStopPanel by polling a remote panel Pi
// over HTTP. Poll() returns nil on any network or protocol error so the
// arena degrades gracefully when a panel is unreachable.
type NetworkEStopPanel struct {
	url    string
	client *http.Client
}

// Compile-time interface assertion.
var _ EStopPanel = (*NetworkEStopPanel)(nil)

// NewNetworkEStopPanel constructs a panel client for the given host.
// host may include a scheme and port, e.g. "http://10.0.100.11:8765",
// or be a bare "host:port" — the constructor normalises it.
func NewNetworkEStopPanel(host string) *NetworkEStopPanel {
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return &NetworkEStopPanel{
		url:    strings.TrimRight(host, "/") + "/poll",
		client: &http.Client{Timeout: 200 * time.Millisecond},
	}
}

// Poll calls GET /poll on the panel Pi and returns the currently-active stops.
// Returns nil on any error (connection failure, timeout, non-200, bad JSON).
func (n *NetworkEStopPanel) Poll() []EStopEvent {
	resp, err := n.client.Get(n.url)
	if err != nil {
		log.Printf("NetworkEStopPanel: GET %s: %v", n.url, err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("NetworkEStopPanel: GET %s returned %d", n.url, resp.StatusCode)
		return nil
	}
	var events []EStopEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		log.Printf("NetworkEStopPanel: decode response from %s: %v", n.url, err)
		return nil
	}
	return events
}
