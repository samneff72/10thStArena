// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

// Go version 1.22 or newer is required.
//go:build go1.22

package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/team841/bioarena/field"
	"github.com/team841/bioarena/hardware"
	"github.com/team841/bioarena/web"
	"gopkg.in/yaml.v3"
)

const eventDbPath = "./event.db"
const configPath = "./config.yaml"

// EStopPanelConfig holds connection info for a hardware e-stop panel Pi.
type EStopPanelConfig struct {
	Host string `yaml:"host"`
}

// Config is the in-memory representation of config.yaml.
type Config struct {
	AutoDurationSec             int              `yaml:"auto_duration_seconds"`
	PauseDurationSec            int              `yaml:"pause_duration_seconds"`
	TeleopDurationSec           int              `yaml:"teleop_duration_seconds"`
	WarningRemainingDurationSec int              `yaml:"warning_remaining_seconds"`
	HttpPort               int              `yaml:"http_port"`
	NetworkSecurityEnabled bool             `yaml:"network_security_enabled"`
	FieldLightsDriver      string           `yaml:"field_lights_driver"`
	FieldLightsPort        string           `yaml:"field_lights_port"`
	FieldLightsBaud        int              `yaml:"field_lights_baud"`
	FieldLightsCommand     string           `yaml:"field_lights_command"`
	RedEStopPanel          EStopPanelConfig `yaml:"red_estop_panel"`
	BlueEStopPanel         EStopPanelConfig `yaml:"blue_estop_panel"`
}

func defaultConfig() *Config {
	return &Config{
		AutoDurationSec:             20,
		PauseDurationSec:            3,
		TeleopDurationSec:           140,
		WarningRemainingDurationSec: 30,
		HttpPort:           8080,
		FieldLightsBaud:    9600,
		FieldLightsCommand: "START\n",
	}
}

// loadConfig reads config.yaml. If the file is absent, defaults are returned.
// Unknown keys produce a fatal error to catch typos early.
func loadConfig(path string) (*Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		log.Printf("No config.yaml found at %s — using defaults.", path)
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // error on unrecognized keys — catches typos
	if err = dec.Decode(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// buildFieldLights constructs the FieldLights driver from config.
func buildFieldLights(cfg *Config) hardware.FieldLights {
	switch cfg.FieldLightsDriver {
	case "", "none":
		return &hardware.NoopFieldLights{}
	case "serial":
		baud := cfg.FieldLightsBaud
		if baud == 0 {
			baud = 9600
		}
		cmd := cfg.FieldLightsCommand
		if cmd == "" {
			cmd = "START\n"
		}
		sl, err := hardware.NewSerialFieldLights(cfg.FieldLightsPort, baud, cmd)
		if err != nil {
			log.Fatalf("serial field lights: %v", err)
		}
		return sl
	default:
		log.Fatalf("unknown field_lights_driver: %q", cfg.FieldLightsDriver)
		return nil
	}
}

// Main entry point for the application.
func main() {
	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalln("Error loading config.yaml:", err)
	}

	arena, err := field.NewArena(eventDbPath)
	if err != nil {
		log.Fatalln("Error during startup:", err)
	}

	// Set a default admin password on the very first run so the field is not
	// open to anyone on the network. Can be changed later via the Settings page.
	if arena.EventSettings.AdminPassword == "" {
		arena.EventSettings.AdminPassword = "bioarena"
		if err = arena.Database.UpdateEventSettings(arena.EventSettings); err != nil {
			log.Fatalln("Error saving default admin password:", err)
		}
		log.Println("Admin password set to default: bioarena  (change via Settings page)")
	}

	// Apply timing, network, and hardware config from config.yaml, seeding the DB
	// on every startup so that config.yaml is the authoritative source.
	arena.EventSettings.AutoDurationSec = cfg.AutoDurationSec
	arena.EventSettings.PauseDurationSec = cfg.PauseDurationSec
	arena.EventSettings.TeleopDurationSec = cfg.TeleopDurationSec
	arena.EventSettings.WarningRemainingDurationSec = cfg.WarningRemainingDurationSec
	arena.EventSettings.NetworkSecurityEnabled = cfg.NetworkSecurityEnabled
	arena.EventSettings.RedEStopPanelAddress = cfg.RedEStopPanel.Host
	arena.EventSettings.BlueEStopPanelAddress = cfg.BlueEStopPanel.Host
	if err = arena.Database.UpdateEventSettings(arena.EventSettings); err != nil {
		log.Fatalln("Error saving config to DB:", err)
	}
	if err = arena.LoadSettings(); err != nil {
		log.Fatalln("Error reloading settings:", err)
	}

	arena.FieldLights = buildFieldLights(cfg)

	// On SIGTERM/SIGINT: disable all robots and wait one DS packet cycle before exiting
	// so connected driver stations receive a clean disabled packet.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Shutting down — disabling all robots")
		arena.DisableAll()
		time.Sleep(600 * time.Millisecond)
		os.Exit(0)
	}()

	// Start the web server in a separate goroutine.
	webServer := web.NewWeb(arena)
	go webServer.ServeWebInterface(cfg.HttpPort)

	// Run the arena state machine in the main thread.
	arena.Run()
}
