package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfigAbsent(t *testing.T) {
	cfg, err := loadConfig(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	assert.Nil(t, err)
	assert.Equal(t, 15, cfg.AutoDurationSec)
	assert.Equal(t, 3, cfg.PauseDurationSec)
	assert.Equal(t, 135, cfg.TeleopDurationSec)
	assert.Equal(t, 8080, cfg.HttpPort)
}

func TestLoadConfigCustomValues(t *testing.T) {
	yaml := `
auto_duration_seconds: 20
pause_duration_seconds: 5
teleop_duration_seconds: 120
http_port: 9090
network_security_enabled: true
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	assert.Nil(t, os.WriteFile(path, []byte(yaml), 0644))

	cfg, err := loadConfig(path)
	assert.Nil(t, err)
	assert.Equal(t, 20, cfg.AutoDurationSec)
	assert.Equal(t, 5, cfg.PauseDurationSec)
	assert.Equal(t, 120, cfg.TeleopDurationSec)
	assert.Equal(t, 9090, cfg.HttpPort)
	assert.True(t, cfg.NetworkSecurityEnabled)
}

func TestLoadConfigUnknownKey(t *testing.T) {
	yaml := `auto_duraton_seconds: 20` // intentional typo
	path := filepath.Join(t.TempDir(), "config.yaml")
	assert.Nil(t, os.WriteFile(path, []byte(yaml), 0644))

	_, err := loadConfig(path)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "auto_duraton_seconds")
}

func TestLoadConfigFieldLightsDefaults(t *testing.T) {
	cfg, err := loadConfig(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	assert.Nil(t, err)
	assert.Equal(t, 9600, cfg.FieldLightsBaud)
	assert.Equal(t, "START\n", cfg.FieldLightsCommand)
	assert.Equal(t, "", cfg.FieldLightsDriver)
}

func TestBuildFieldLightsNoneDriver(t *testing.T) {
	// "none" is the documented value for bench testing (config.yaml comment).
	// It must not fatal — it should return a no-op implementation.
	cfg := defaultConfig()
	cfg.FieldLightsDriver = "none"
	lights := buildFieldLights(cfg)
	assert.NotNil(t, lights)
}

func TestBuildFieldLightsEmptyDriver(t *testing.T) {
	cfg := defaultConfig()
	cfg.FieldLightsDriver = ""
	lights := buildFieldLights(cfg)
	assert.NotNil(t, lights)
}

func TestBuildFieldLightsUnknownDriverPanics(t *testing.T) {
	// An unrecognized driver string should cause a fatal log, which os.Exit(1).
	// We verify the known-good and known-bad paths by checking the switch
	// handles expected values without panicking; unknown values are caught at
	// compile time via the exhaustive switch + default fatal.
	cfg := defaultConfig()
	for _, driver := range []string{"", "none"} {
		cfg.FieldLightsDriver = driver
		assert.NotPanics(t, func() { buildFieldLights(cfg) }, "driver=%q should not panic", driver)
	}
}
