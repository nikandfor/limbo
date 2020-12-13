package textproto

import (
	"reflect"
	"strings"
	"sync"
)

type (
	structMap struct {
		fs map[string]structField
	}

	structField struct {
		I    int
		Name string

		OmitEmpty bool
	}
)

var (
	mu   sync.Mutex
	maps map[reflect.Type]structMap
)

func getStructMap(t reflect.Type) structMap {
	defer mu.Unlock()
	mu.Lock()

	s, ok := maps[t]

	if !ok {
		s = makeStructMap(t)
		maps[t] = s
	}

	return s
}

func makeStructMap(t reflect.Type) (s structMap) {
	var b []byte

	ff := t.NumField()

	for i := 0; i < ff; i++ {
		f := t.Field(i)

		sf := structField{
			I: i,
		}

		tags := strings.Split(f.Tag.Get("textproto"), ",")

		if len(tags) != 0 && tags[0] != "" {
			sf.Name = tags[0]
		} else {
			st := 0
			for i, c := range f.Name {
				if i != 0 && isUpper(c) {
					b = append(b, f.Name[st:i]...)
					b = append(b, '-')
					st = i
				}
			}
			if st == 0 {
				sf.Name = f.Name
			} else {
				b = append(b, f.Name[st:len(f.Name)]...)
				sf.Name = string(b)
				b = b[:0]
			}
		}

		if len(tags) > 1 {
			for _, t := range tags[1:] {
				if t == "omitempty" {
					sf.OmitEmpty = true
				}
			}
		}

		s.fs[sf.Name] = sf
	}

	return s
}

func isUpper(c rune) bool {
	return c >= 'A' && c <= 'Z'
}
