package textproto

import (
	"errors"
	"io"
)

type (
	Writer struct {
		w io.Writer
		b []byte

		state byte
	}
)

func NewWriter(w io.Writer) *Writer {
	return &Writer{
		w: w,
	}
}

func (w *Writer) KeyString(k string) (err error) {
	if w.state != 0 {
		return errors.New("key is not expected")
	}

	st := 0

	addk := func(i int) {
		w.b = append(w.b, toUpper(k[st]))
		w.b = append(w.b, k[st+1:i]...)
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

	w.state = 'v'

	return nil
}

func (w *Writer) ValueString(v string) (err error) {
	if w.state != 'v' {
		return errors.New("value is not expected")
	}

	st := 0

	addv := func(i int) {
		w.b = append(w.b, v[st:i]...)
	}

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
	w.state = 0

	return nil
}

func (w *Writer) Key(k []byte) error {
	return w.KeyString(bytesToString(k))
}

func (w *Writer) Value(v []byte) error {
	return w.ValueString(bytesToString(v))
}

func (w *Writer) PairStrings(k, v string) (err error) {
	err = w.KeyString(k)
	if err != nil {
		return err
	}

	err = w.ValueString(v)
	if err != nil {
		return err
	}

	return nil
}

func (w *Writer) Pair(k, v []byte) error {
	return w.PairStrings(bytesToString(k), bytesToString(v))
}
