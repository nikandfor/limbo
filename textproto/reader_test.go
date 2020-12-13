package textproto

import (
	"strings"
	"testing"

	"github.com/nikandfor/tlog"
	"github.com/stretchr/testify/assert"
)

func TestReader(t *testing.T) {
	data := `Key: value
Long-key:  long
 value

	very
no: newline`

	tl = tlog.NewTestLogger(t, "", nil)

	r := NewReader(strings.NewReader(data))

loop:
	for i := 0; r.Next(); i++ {
		k := r.Key()
		v := r.Value()

		switch i {
		case 0:
			assert.Equal(t, "Key", string(k))
			assert.Equal(t, "value", string(v))
		case 1:
			assert.Equal(t, "Long-Key", string(k))
			assert.Equal(t, `long
value
very`, string(v))
		case 2:
			assert.Equal(t, "No", string(k))
			assert.Equal(t, "newline", string(v))
		default:
			t.Errorf("more lines than expected: %q: %q", k, v)
			break loop
		}
	}

	assert.NoError(t, r.Err())
}
