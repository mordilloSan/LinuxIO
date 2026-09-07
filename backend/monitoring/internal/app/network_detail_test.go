package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseInterrupts(t *testing.T) {
	raw := `           CPU0       CPU1
  0:         10          2   IO-APIC   2-edge      timer
NMI:          1          3   Non-maskable interrupts
LOC:        100        200   Local timer interrupts
`

	items := parseInterrupts(raw)

	require.Len(t, items, 3)
	assert.Equal(t, "0", items[0].IRQ)
	assert.Equal(t, uint64(12), items[0].Total)
	assert.Equal(t, []uint64{10, 2}, items[0].CPUCounts)
	assert.Equal(t, "IO-APIC 2-edge timer", items[0].Description)
	assert.Equal(t, "NMI", items[1].IRQ)
	assert.Equal(t, uint64(4), items[1].Total)
	assert.Equal(t, "Non-maskable interrupts", items[1].Description)
}
