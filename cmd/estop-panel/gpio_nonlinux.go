//go:build !linux

package main

import "log"

// openGPIO on non-Linux returns a no-op reader.
// The binary compiles but reads no physical pins.
func openGPIO(_ string, _ PinConfig, _ string) (gpioReader, error) {
	log.Println("WARNING: GPIO not available on this platform — polls will return empty")
	return newNoopReader(), nil
}
