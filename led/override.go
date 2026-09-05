// Portions Copyright Team 841. All Rights Reserved.
//
// Practice-field overrides for the upstream Hub LED controller.
//
// This file is a bioarena addition. Every other file in this package is a byte-identical
// copy of upstream cheesy-arena, so that `git diff upstream/main -- led/` stays empty and
// upstream lighting changes can be taken with a checkout. Keep it that way: put local
// behaviour here rather than editing the ported files.

package led

import "fmt"

// FixtureSpec addresses one physical fixture on the DMX line.
type FixtureSpec struct {
	Universe     int // 1-based DMX universe
	StartAddress int // 1-based DMX start address
}

// ChannelsPerFixture is the channel span the renderer writes for every fixture,
// regardless of how many pixels the physical unit actually has.
const ChannelsPerFixture = channelsPerFixture

// SetLayout replaces the fixture layout with a practice-field one. Passing no fixtures
// for an alliance disables output for that alliance.
//
// A fixture with fewer pixels than the renderer produces still works: the renderer
// always writes ChannelsPerFixture channels from the start address, and a physical unit
// simply reads the first channels it cares about, ignoring the rest. A single-pixel RGB
// fixture reads the first three and shows the colour of the zone's first pixel.
//
// The consequence is that fixtures must be spaced at least ChannelsPerFixture apart, or
// an earlier fixture's write will clobber a later one's channels. That is validated here
// rather than left to produce silently wrong colours on the field.
func (controller *Controller) SetLayout(red, blue []FixtureSpec) error {
	redFixtures, err := buildFixtures(red, "red")
	if err != nil {
		return err
	}
	blueFixtures, err := buildFixtures(blue, "blue")
	if err != nil {
		return err
	}
	if err := checkOverlap(append(append([]FixtureSpec{}, red...), blue...)); err != nil {
		return err
	}

	controller.fixtures = fixtureLayout{red: redFixtures, blue: blueFixtures}
	controller.universes = map[int]*universe{}
	return nil
}

// UseDefaultLayout restores upstream's full-field layout: eight fixtures per alliance
// across four sides of each goal, all on one universe.
//
// Upstream also carries a two-universe variant, selected by its own SetUniverseMode.
// Nothing here calls that yet -- the practice-field path splits universes per alliance
// through SetLayout instead -- so a field wanting the full layout across two universes
// would wire SetUniverseMode to a setting rather than change this.
func (controller *Controller) UseDefaultLayout() {
	controller.fixtures = singleUniverseFixtureLayout
	controller.universes = map[int]*universe{}
}

func buildFixtures(specs []FixtureSpec, alliance string) ([]fixture, error) {
	fixtures := make([]fixture, 0, len(specs))
	for i, spec := range specs {
		if spec.Universe <= 0 {
			return nil, fmt.Errorf("%s fixture %d: universe must be 1 or greater, got %d", alliance, i+1, spec.Universe)
		}
		startIndex := spec.StartAddress - 1
		if startIndex < 0 || startIndex+channelsPerFixture > universeChannelCount {
			return nil, fmt.Errorf(
				"%s fixture %d: start address %d leaves less than %d channels in the universe",
				alliance, i+1, spec.StartAddress, channelsPerFixture,
			)
		}
		fixtures = append(fixtures, fixture{fixtureId(i), spec.Universe, spec.StartAddress})
	}
	return fixtures, nil
}

// checkOverlap rejects layouts whose fixtures would write over each other.
func checkOverlap(specs []FixtureSpec) error {
	for i, a := range specs {
		for j, b := range specs {
			if i >= j || a.Universe != b.Universe {
				continue
			}
			if a.StartAddress < b.StartAddress+channelsPerFixture &&
				b.StartAddress < a.StartAddress+channelsPerFixture {
				return fmt.Errorf(
					"fixtures at addresses %d and %d overlap in universe %d; space them at least %d channels apart",
					a.StartAddress, b.StartAddress, a.Universe, channelsPerFixture,
				)
			}
		}
	}
	return nil
}

// Fallback describes how much a practice field's fixtures can actually render.
type Fallback int

const (
	// FallbackFull runs upstream's sequences unchanged. Correct for addressable
	// fixtures with the full pixel count.
	FallbackFull Fallback = iota

	// FallbackSolid collapses per-pixel sequences to a solid alliance colour. Correct
	// for single-pixel or non-addressable fixtures, where a fill or sweep would
	// otherwise show whichever colour the first pixel happened to be mid-animation.
	// Pulses are kept: they vary brightness uniformly, which any dimmable fixture can
	// render.
	FallbackSolid

	// FallbackBinary additionally flattens pulses, for fixtures that are on or off with
	// no usable dimming.
	FallbackBinary
)

var fallbackNames = map[Fallback]string{
	FallbackFull:   "full",
	FallbackSolid:  "solid",
	FallbackBinary: "binary",
}

func (fallback Fallback) String() string {
	if name, ok := fallbackNames[fallback]; ok {
		return name
	}
	return "full"
}

// ParseFallback converts the stored representation, defaulting to full.
func ParseFallback(name string) (Fallback, error) {
	for fallback, fallbackName := range fallbackNames {
		if fallbackName == name {
			return fallback, nil
		}
	}
	return FallbackFull, fmt.Errorf("invalid LED fallback %q", name)
}

// ApplyFallback maps a mode onto what the configured fixtures can actually show.
//
// solidMode is the zone's own solid colour -- RedMode for the red zone, BlueMode for the
// blue one. It is a parameter because several sequences are alliance-agnostic: the side
// tests and the rainbow take their colour from the zone they run in, so the substitution
// cannot be derived from the mode alone.
//
// A pure function rather than controller state, so the substitution is testable on its
// own and the caller can see the mode it ends up sending.
func ApplyFallback(fallback Fallback, mode, solidMode Mode) Mode {
	if fallback == FallbackFull {
		return mode
	}

	switch mode {
	case RedStartupMode, BlueStartupMode,
		RedAdvantageMode, BlueAdvantageMode,
		RainbowMode,
		Side1TestMode, Side2TestMode, Side3TestMode, Side4TestMode:
		// Per-pixel sequences. On a single-pixel fixture these would show whichever
		// colour the first pixel happened to be mid-animation, which for a sweep is a
		// flicker and for a fill is a fade from black.
		mode = solidMode
	}

	if fallback == FallbackBinary {
		switch mode {
		case RedPulseMode, BluePulseMode:
			mode = solidMode
		}
	}

	return mode
}
