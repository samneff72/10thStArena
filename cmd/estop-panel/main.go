// Binary estop-panel runs on a Raspberry Pi attached to hardware e-stop
// buttons. It reads GPIO pins and serves the active stops over HTTP so the
// main bioarena can poll it.
//
// Configuration is read from estop-panel.yaml in the working directory.
// POST /config replaces the full config, re-opens GPIO, and persists to disk.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/team841/bioarena/hardware"
	"gopkg.in/yaml.v3"
)

// PinConfig maps logical e-stop roles to BCM GPIO pin numbers.
// A value of 0 means "not wired" and is skipped.
type PinConfig struct {
	Station1EStop int `yaml:"station1_estop" json:"station1_estop"`
	Station1AStop int `yaml:"station1_astop" json:"station1_astop"`
	Station2EStop int `yaml:"station2_estop" json:"station2_estop"`
	Station2AStop int `yaml:"station2_astop" json:"station2_astop"`
	Station3EStop int `yaml:"station3_estop" json:"station3_estop"`
	Station3AStop int `yaml:"station3_astop" json:"station3_astop"`
	FieldEStop    int `yaml:"field_estop"    json:"field_estop"`
}

// PanelConfig is the in-memory representation of estop-panel.yaml.
type PanelConfig struct {
	Alliance string    `yaml:"alliance"  json:"alliance"`  // "red" or "blue"
	HTTPPort int       `yaml:"http_port" json:"http_port"`
	GpioChip string    `yaml:"gpio_chip" json:"gpio_chip"`
	Pins     PinConfig `yaml:"pins"      json:"pins"`
}

// gpioReader abstracts GPIO access; implemented per platform.
type gpioReader interface {
	// Read returns currently active (active-low) stops.
	Read() []hardware.EStopEvent
	// Close releases all opened GPIO lines.
	Close()
}

// noopReader is returned when GPIO is unavailable.
type noopReader struct{}

func newNoopReader() gpioReader                   { return &noopReader{} }
func (n *noopReader) Read() []hardware.EStopEvent { return nil }
func (n *noopReader) Close()                      {}

const cfgPath = "estop-panel.yaml"

var (
	mu     sync.RWMutex
	cfg    PanelConfig
	reader gpioReader
)

func main() {
	if err := loadConfig(); err != nil {
		log.Fatalf("load config: %v", err)
	}

	r, err := openGPIO(cfg.GpioChip, cfg.Pins, cfg.Alliance)
	if err != nil {
		log.Printf("WARNING: could not open GPIO: %v — polls will return empty", err)
		r = newNoopReader()
	}
	reader = r

	mux := http.NewServeMux()
	mux.HandleFunc("/poll", handlePoll)
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/config", handleConfig)

	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	log.Printf("estop-panel (%s) listening on %s", cfg.Alliance, addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("HTTP server: %v", err)
	}
}

func loadConfig() error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, &cfg)
}

func saveConfig() error {
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0644)
}

// stationNames returns [station1, station2, station3] names for the given alliance.
func stationNames(alliance string) [3]string {
	if alliance == "red" {
		return [3]string{"R1", "R2", "R3"}
	}
	return [3]string{"B1", "B2", "B3"}
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func handlePoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mu.RLock()
	events := reader.Read()
	mu.RUnlock()
	if events == nil {
		events = []hardware.EStopEvent{} // return [] not null
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		mu.RLock()
		c := cfg
		mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(c)
	case http.MethodPost:
		var update PanelConfig
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Open new GPIO outside the lock to avoid holding it during I/O.
		newReader, err := openGPIO(update.GpioChip, update.Pins, update.Alliance)
		if err != nil {
			log.Printf("WARNING: re-opening GPIO after config update: %v", err)
			newReader = newNoopReader()
		}
		mu.Lock()
		old := reader
		cfg = update
		reader = newReader
		saveErr := saveConfig()
		mu.Unlock()
		old.Close()
		if saveErr != nil {
			log.Printf("WARNING: saving config: %v", saveErr)
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
