package khaleesi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModify(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		input    string
		modified bool
	}{
		{"", false},
		{"Lorem ipsum dolor sit amet", false},
		{"Съешь еще этих мягких французских булок, да выпей чаю", true},
	}

	kh, err := New()
	require.NoError(t, err)

	for _, tt := range testCases {
		_, modified := kh.Modify(tt.input)
		assert.Equal(t, tt.modified, modified)
	}
}
