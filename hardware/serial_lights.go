package hardware

import (
	"fmt"
	"log"

	"go.bug.st/serial"
)

// SerialFieldLights sends a single ASCII command over serial when a match starts.
// The receiving device (Arduino) handles all subsequent lighting and sound sequences
// autonomously. All other LightingState changes are silently ignored.
type SerialFieldLights struct {
	port    serial.Port
	command string
}

// Compile-time interface assertion.
var _ FieldLights = (*SerialFieldLights)(nil)

// NewSerialFieldLights opens the named serial port and returns a driver that
// sends command exactly once when the match enters PhaseAuto.
func NewSerialFieldLights(portName string, baud int, command string) (*SerialFieldLights, error) {
	port, err := serial.Open(portName, &serial.Mode{BaudRate: baud})
	if err != nil {
		return nil, fmt.Errorf("open serial port %q: %w", portName, err)
	}
	log.Printf("SerialFieldLights: opened %s at %d baud", portName, baud)
	return &SerialFieldLights{port: port, command: command}, nil
}

// SetState sends the configured command when the match enters PhaseAuto.
// All other phase transitions are ignored.
func (s *SerialFieldLights) SetState(state LightingState) error {
	if state.Phase == PhaseAuto {
		_, err := fmt.Fprint(s.port, s.command)
		return err
	}
	return nil
}
