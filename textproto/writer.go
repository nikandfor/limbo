package textproto

import "io"

type (
	Writer struct {
		w io.Writer
		b []byte
	}
)

func NewWriter(w io.Writer) *Writer {
	return &Writer{
		w: w,
	}
}

func (w *Writer) WritePairStrings(k, v string) (err error) {
	st := 0

	addk := func(i int) {
		w.b = append(w.b, toUpper(k[st]))
		w.b = append(w.b, k[st+1:i]...)
	}

	addv := func(i int) {
		w.b = append(w.b, v[st:i]...)
	}

	for i := 0; i < len(k); {
		switch k[i] {
		case '-':
			i++

			addk(i)
			st = i
		default:
			i++
		}
	}
	addk(len(k))

	w.b = append(w.b, ':', ' ')

	st = 0

	for i := 0; i < len(v); {
		switch v[i] {
		case '\n':
			i++

			addv(i)
			st = i

			if i < len(v) {
				w.b = append(w.b, ' ')
			}
		default:
			i++
		}
	}
	addv(len(v))

	if l := len(w.b) - 1; w.b[l] != '\n' {
		w.b = append(w.b, '\n')
	}

	_, err = w.w.Write(w.b)

	w.b = w.b[:0]

	return
}

func (w *Writer) WritePair(k, v []byte) error {
	return w.WritePairStrings(bytesToString(k), bytesToString(v))
}
