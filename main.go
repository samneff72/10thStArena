// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

// Go version 1.22 or newer is required.
//go:build go1.22

package main

import (
	"bytes"
	"errors"
	"log"
	"os"

	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/web"
	"gopkg.in/yaml.v3"
)

const eventDbPath = "./event.db"
const configPath = "./config.yaml"

// EStopPanelConfig holds connection info for a hardware e-stop panel (Phase 4).
type EStopPanelConfig struct {
	Driver string `yaml:"driver"`
	Host   string `yaml:"host"`
}

// Config is the in-memory representation of config.yaml.
type Config struct {
	AutoDurationSec        int              `yaml:"auto_duration_seconds"`
	PauseDurationSec       int              `yaml:"pause_duration_seconds"`
	TeleopDurationSec      int              `yaml:"teleop_duration_seconds"`
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
		AutoDurationSec:    15,
		PauseDurationSec:   3,
		TeleopDurationSec:  135,
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

	// Apply timing and network config from config.yaml, seeding the DB on every
	// startup so that config.yaml is the authoritative source for these values.
	arena.EventSettings.AutoDurationSec = cfg.AutoDurationSec
	arena.EventSettings.PauseDurationSec = cfg.PauseDurationSec
	arena.EventSettings.TeleopDurationSec = cfg.TeleopDurationSec
	arena.EventSettings.NetworkSecurityEnabled = cfg.NetworkSecurityEnabled
	if err = arena.Database.UpdateEventSettings(arena.EventSettings); err != nil {
		log.Fatalln("Error saving config to DB:", err)
	}
	if err = arena.LoadSettings(); err != nil {
		log.Fatalln("Error reloading settings:", err)
	}

	// Start the web server in a separate goroutine.
	webServer := web.NewWeb(arena)
	go webServer.ServeWebInterface(cfg.HttpPort)

	// Run the arena state machine in the main thread.
	arena.Run()
}
