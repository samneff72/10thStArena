package hardware

// Compile-time interface assertions.
var _ FieldLights = (*NoopFieldLights)(nil)
var _ EStopPanel = (*NoopEStopPanel)(nil)
var _ FieldEStopPanel = (*NoopFieldEStopPanel)(nil)

// NoopFieldLights discards all state changes. Used when no lighting driver is configured.
type NoopFieldLights struct{}

func (n *NoopFieldLights) SetState(_ LightingState) error { return nil }

// NoopEStopPanel reports no stops. Used when no hardware panel is configured.
type NoopEStopPanel struct{}

func (n *NoopEStopPanel) Poll() []EStopEvent { return nil }

// NoopFieldEStopPanel never triggers. Used when no GPIO pin is configured.
type NoopFieldEStopPanel struct{}

func (n *NoopFieldEStopPanel) Triggered() bool { return false }
func (n *NoopFieldEStopPanel) Clear()          {}
