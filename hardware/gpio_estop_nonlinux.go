//go:build !linux

package hardware

import "fmt"

// NewGpioFieldEStopPanel is not supported on non-Linux platforms.
// Use field_estop_pin: 0 in config.yaml (or omit it) to use NoopFieldEStopPanel instead.
func NewGpioFieldEStopPanel(chip string, pin int) (*GpioFieldEStopPanel, error) {
	return nil, fmt.Errorf("GPIO field e-stop not supported on this platform (Linux/Raspberry Pi only)")
}
