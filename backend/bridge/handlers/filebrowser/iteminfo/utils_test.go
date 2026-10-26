package iteminfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSortNamesIsTotalAndStable(t *testing.T) {
	// 9 < 10 < 1a < 9 under the old comparator; case-only ties used to stay unordered.
	names := []string{"b", "1a", "B", "10", "a", "9", "A", "10.txt"}
	SortNames(names)
	assert.Equal(t, []string{"9", "10", "10.txt", "1a", "A", "a", "B", "b"}, names)

	items := []ItemInfo{{Name: "b"}, {Name: "B"}, {Name: "2"}}
	SortItems(items)
	assert.Equal(t, []string{"2", "B", "b"}, []string{items[0].Name, items[1].Name, items[2].Name})
}
