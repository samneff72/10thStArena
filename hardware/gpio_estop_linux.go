//go:build linux

package hardware

import (
	"fmt"
	"log"

	"github.com/warthog618/go-gpiocdev"
)

// NewGpioFieldEStopPanel opens the named GPIO chip and pin number.
// chip is "gpiochip0" on Raspberry Pi; pin is the BCM GPIO number from config.yaml.
// The line is configured as input with the Pi's internal pull-up resistor enabled.
func NewGpioFieldEStopPanel(chip string, pin int) (*GpioFieldEStopPanel, error) {
	line, err := gpiocdev.RequestLine(chip, pin,
		gpiocdev.AsInput,
		gpiocdev.WithPullUp,
	)
	if err != nil {
		return nil, fmt.Errorf("open GPIO chip %q pin %d: %w", chip, pin, err)
	}
	log.Printf("GpioFieldEStopPanel: opened %s pin %d (active-low, pull-up)", chip, pin)
	return &GpioFieldEStopPanel{reader: line}, nil
}
