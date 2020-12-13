package textproto

import (
	"errors"
	"io"

	"github.com/nikandfor/tlog"
)

type (
	Reader struct {
		r io.Reader

		b    []byte
		read int

		err error
		key []byte
		val []byte
	}
)

var tl *tlog.Logger

func NewReader(r io.Reader) *Reader {
	return &Reader{
		r: r,
		b: make([]byte, 1000),
	}
}

func (r *Reader) Next() bool {
	if r.err != nil {
		return false
	}

	b := r.b
	r.key = r.key[:0]
	r.val = r.val[:0]

	i := 0

	tl.Printw("start next", "i", i, "read", r.read, "buf_size", cap(b))
	defer func() {
		tl.Printw("end of next", "i", i, "read", r.read, "key", r.key, "value", r.val)
	}()

	vst := 0
	vaddnl := false

	addv := func(i int) {
		if vaddnl {
			r.val = append(r.val, '\n')
		}

		r.val = append(r.val, b[vst:i]...)
	}

	kst := 0

	addk := func(i int) {
		r.key = append(r.key, toUpper(b[kst]))
		r.key = append(r.key, b[kst+1:i]...)
	}

	state := 'k'

more:
	if r.read == cap(b) {
		b = append(b, 0, 0, 0, 0)
	}

	n, err := r.r.Read(b[r.read:cap(b)])
	r.read += n
	tl.Printw("read", "i", i, "read", r.read, "n", n)
	switch {
	case err == nil:
	case err == io.EOF && n == 0 && i == r.read:
		addv(i)

		state = 'e'
		err = nil
	case err == io.EOF:
		err = nil
	default:
		r.err = err

		return false
	}

	// key
	if state == 'k' {
	loop:
		for i < r.read {
			switch b[i] {
			case ':':
				addk(i)

				i++
				state = 's'

				break loop
			case '-':
				i++

				addk(i)

				kst = i
			case ' ', '\t', '\n', '\r':
				r.err = errors.New("reading key: unexpected char")

				return false
			default:
				i++
			}
		}
	}

	// spaces
	if state == 's' {
	loop2:
		for i < r.read {
			switch b[i] {
			case ' ', '\t', '\n', '\r':
				i++

			default:
				vst = i
				state = 'v'

				break loop2
			}
		}
	}

	// value
	if state == 'v' {
	loop3:
		for i < r.read {
			switch b[i] {
			case '\n', '\r':
				addv(i)

				i++
				state = 'w'
				vaddnl = true

				break loop3
			default:
				i++
			}
		}
	}

	// wrapping
	if state == 'w' {
	loop4:
		for i < r.read {
			switch b[i] {
			case ' ', '\t':
				i++
				state = 's'

				break loop4
			case '\n', '\r':
				i++
			default:
				state = 'e'

				break loop4
			}
		}
	}

	// end
	if state == 'e' {
		if i < r.read {
			copy(b, b[i:r.read])
		}

		r.b = b
		r.read = r.read - i

		return len(r.key) != 0
	}

	goto more
}

func (r *Reader) Key() []byte {
	if r.err != nil {
		return nil
	}

	return r.key
}

func (r *Reader) Value() []byte {
	if r.err != nil {
		return nil
	}

	return r.val
}

func (r *Reader) Err() error {
	return r.err
}

func toUpper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		c -= 'a' - 'A'
	}

	return c
}
