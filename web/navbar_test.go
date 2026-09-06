package web

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The header carries the address someone needs to reach this controller. The field address
// is the same on every field and is not a route anyone's laptop has, so without this the
// answer is found by walking to the Pi with a keyboard.
func TestNavbarShowsReachableAddress(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.getHttpResponse("/match_play")
	assert.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()

	assert.Contains(t, body, "nav-lan-value", "the header should carry the LAN address element")

	// Whatever this machine has, the element must not render empty -- an empty box in the
	// header is worse than the honest "field only".
	start := strings.Index(body, `class="nav-lan-value"`)
	assert.Greater(t, start, 0)
	segment := body[start : start+120]
	assert.NotContains(t, segment, "></span>", "the address element rendered empty")
}
