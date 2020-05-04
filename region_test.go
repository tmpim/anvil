package anvil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCoord(t *testing.T) {
	c := Coord{500, 64, -500}
	chk := c.Chunk()

	assert.Equal(t, 31, chk.X)
	assert.Equal(t, 4, chk.Y)
	assert.Equal(t, -32, chk.Z)

	r := c.Region()
	assert.Equal(t, 0, r.X)
	assert.Equal(t, -1, r.Z)
}
