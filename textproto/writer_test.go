package textproto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriter(t *testing.T) {
	var buf bytes.Buffer

	w := NewWriter(&buf)

	err := w.WritePair([]byte("key"), []byte("value"))
	assert.NoError(t, err)

	err = w.WritePairStrings("complex-key", `long value
multiple
	lines`)
	assert.NoError(t, err)

	assert.Equal(t, `Key: value
Complex-Key: long value
 multiple
 	lines
`, buf.String())
}
