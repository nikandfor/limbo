package deb

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"io"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"

	"github.com/blakesmith/ar"
	"github.com/nikandfor/errors"
	"github.com/nikandfor/tlog"
	"github.com/xi2/xz"

	"github.com/rndcenter/limbo/textproto"
)

type (
	Control struct {
		Package       string
		Version       string
		Architecture  string
		InstalledSize int64
		Section       string   `deb:",omitempty" json:",omitempty"`
		Priority      string   `deb:",omitempty" json:",omitempty"`
		Maintainer    string   `deb:",omitempty" json:",omitempty"`
		Vendor        string   `deb:",omitempty" json:",omitempty"`
		Depends       []string `deb:",omitempty" json:",omitempty"`
		PreDepends    []string `deb:",omitempty" json:",omitempty"`
		Recommends    []string `deb:",omitempty" json:",omitempty"`
		Homepage      string   `deb:",omitempty" json:",omitempty"`
		Description   string   `deb:",omitempty" json:",omitempty"`
		Replaces      []string `deb:",omitempty" json:",omitempty"`
		Provides      []string `deb:",omitempty" json:",omitempty"`
		Conflicts     []string `deb:",omitempty" json:",omitempty"`

		ConfFiles []string

		Rest map[string]interface{} `json:"rest,omitempty"`
	}

	Package struct {
		Control      Control
		RestControls map[string]interface{}

		files  map[string]*file
		filesl []*file `tlog:""`

		md5sums bool

		b []byte
	}

	file struct {
		Name string

		MD5sum [md5.Size]byte `tlog:",hex"`

		data []byte
	}

	rawControl struct {
		fs map[string]*rawField
	}

	rawField struct {
		I         int
		Name      string
		OmitEmpty bool
	}
)

var rcontrol *rawControl

func init() {
	rcontrol = &rawControl{
		fs: make(map[string]*rawField),
	}

	r := reflect.TypeOf(Control{})

	var b []byte

	for i := 0; i < r.NumField(); i++ {
		f := r.Field(i)

		rf := rawField{
			I: i,
		}

		tg := strings.Split(f.Tag.Get("deb"), ",")

		if len(tg) != 0 && tg[0] != "" {
			rf.Name = tg[0]
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
				rf.Name = f.Name
			} else {
				b = append(b, f.Name[st:len(f.Name)]...)
				rf.Name = string(b)
				b = b[:0]
			}
		}

		if len(tg) > 1 {
			for _, t := range tg[1:] {
				if t == "omitempty" {
					rf.OmitEmpty = true
				}
			}
		}

		rcontrol.fs[rf.Name] = &rf
	}
}

func Open(fn string) (p *Package, err error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, errors.Wrap(err, "open file")
	}
	defer func() {
		e := f.Close()
		if err == nil {
			err = e
		}
	}()

	p = &Package{
		files: make(map[string]*file),
		b:     make([]byte, 10),
	}

	err = p.readAr(f)
	if err != nil {
		return nil, err
	}

	p.b = p.b[:0]

	return p, nil
}

func (p *Package) readAr(f io.Reader) error {
	a := ar.NewReader(f)

	h, err := a.Next()
	if err != nil {
		return errors.Wrap(err, "read deb version (header)")
	}

	if h.Name != "debian-binary" {
		return errors.New("bad deb format: expected debian-binary got %s", h.Name)
	}

	n, err := a.Read(p.b)
	if err != nil {
		return errors.Wrap(err, "read deb version (content)")
	}

	if !bytes.Equal(p.b[:n], []byte("2.0\n")) && !bytes.Equal(p.b[:n], []byte("2.0")) {
		return errors.New("unsupported version: %q", p.b[:n])
	}

	h, err = a.Next()
	if err != nil {
		return errors.Wrap(err, "read deb control: header")
	}

	if !strings.HasPrefix(h.Name, "control.") {
		return errors.New("bad deb format: expected control.tar got %s", h.Name)
	}

	err = p.readTar(h.Name, a, p.readControl)
	if err != nil {
		return errors.Wrap(err, "read control")
	}

	if tlog.If("noreaddata") {
		return nil
	}

	h, err = a.Next()
	if err != nil {
		return errors.Wrap(err, "read deb data: header")
	}

	if !strings.HasPrefix(h.Name, "data.") {
		return errors.New("bad deb format: expected data.tar got %s", h.Name)
	}

	err = p.readTar(h.Name, a, p.readData)
	if err != nil {
		return errors.Wrap(err, "read data")
	}

	return nil
}

func (p *Package) readTar(n string, r io.Reader, f func(h *tar.Header, r io.Reader) error) (err error) {
	var a *tar.Reader

again:
	switch ext := path.Ext(n); ext {
	case ".tar":
		a = tar.NewReader(r)
	case ".gz":
		g, err := gzip.NewReader(r)
		if err != nil {
			return errors.Wrap(err, "open gzip")
		}
		defer func() {
			e := g.Close()
			if err == nil {
				err = e
			}
		}()

		r = g
		n = strings.TrimSuffix(n, ext)

		goto again
	case ".xz":
		x, err := xz.NewReader(r, 0)
		if err != nil {
			return errors.Wrap(err, "open xz")
		}

		r = x
		n = strings.TrimSuffix(n, ext)

		goto again
	default:
		return errors.New("unsupported file format: %v", ext)
	}

	for {
		h, err := a.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "read header")
		}

		err = f(h, a)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Package) readControl(h *tar.Header, r io.Reader) (err error) {
	tlog.V("fileheader").Printw("control file", "type", tlog.FormatNext("%c"), h.Typeflag, "size", h.Size, "name", h.Name)

	if h.Typeflag != tar.TypeReg {
		// skip
		return nil
	}

	if tlog.If("filecontent") {
		data, err := p.readAll(h, r)
		if err != nil {
			return errors.Wrap(err, "read file content: %v", h.Name)
		}

		tlog.Printf("file content:\n%s", data)

		r = bytes.NewReader(data)
	}

	name := path.Clean(h.Name)

	switch name {
	case "control":
		err = p.Control.Load(r)
	case "md5sums":
		p.md5sums = true
		err = p.parseMD5Sums(r)
	default:
		err = p.readRestControl(name, r)
	}

	err = errors.Wrap(err, "parse %v", h.Name)

	return err
}

func (c *Control) Load(r io.Reader) error {
	if c == nil {
		return errors.New("nil Control")
	}

	v := reflect.ValueOf(c).Elem()

	s := textproto.NewReader(r)

	for s.Next() {
		name := s.Key()
		val := s.Value()

		fr, ok := rcontrol.fs[string(name)]

		if !ok {
			if c.Rest == nil {
				c.Rest = make(map[string]interface{})
			}

			c.Rest[string(name)] = string(val)

			continue
		}

		f := v.Field(fr.I)

		switch f.Kind() {
		case reflect.String:
			f.SetString(string(val))
		case reflect.Int, reflect.Int64:
			q, err := strconv.ParseInt(string(val), 10, 64)
			if err != nil {
				return errors.Wrap(err, "parse %s", name)
			}

			f.SetInt(q)
		case reflect.Slice:
			fvs := strings.Split(string(val), ",")
			for i := range fvs {
				fvs[i] = strings.TrimSpace(fvs[i])
			}

			f.Set(reflect.ValueOf(fvs))
		default:
			return errors.New("unsupported field type: %v", f.Type())
		}
	}

	return nil
}

func (p *Package) readRestControl(n string, r io.Reader) error {
	if p.RestControls == nil {
		p.RestControls = make(map[string]interface{})
	}

	data, err := ioutil.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "read file content")
	}

	p.RestControls[n] = data

	return nil
}

func (p *Package) parseMD5Sums(r io.Reader) (err error) {
	s := bufio.NewScanner(r)

	for s.Scan() {
		l := s.Bytes()
		if len(l) == 0 {
			continue
		}

		name := string(l[32:])
		name = strings.TrimSpace(name)

		f := p.file(name)

		_, err = hex.Decode(f.MD5sum[:], l[:32])
		if err != nil {
			return errors.Wrap(err, "read md5")
		}
	}

	return nil
}

func (p *Package) readData(h *tar.Header, r io.Reader) error {
	tlog.V("fileheader").Printw("data file", "type", tlog.FormatNext("%c"), h.Typeflag, "size", h.Size, "name", h.Name)

	data, err := ioutil.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "read file content")
	}

	var f *file

	if p.md5sums && h.Typeflag == tar.TypeReg {
		f = p.files[path.Clean(h.Name)]
		if f == nil {
			//	return errors.New("%v: no md5sum", h.Name)
			tlog.Printw("no md5sum", "file", h.Name)
		}

		md5sum := md5.Sum(data)
		if f != nil && f.MD5sum != md5sum {
			//	return errors.New("%v: md5sum mismatch", h.Name)

			tlog.Printw("md5sum mismatch", "file", h.Name, "md5sum", md5sum, "debmd5", f.MD5sum)
		}
	} else {
		f = p.file(h.Name)
	}

	f.data = data

	p.filesl = append(p.filesl, f)

	return nil
}

func (p *Package) file(n string) (f *file) {
	n = path.Clean(n)

	f = p.files[n]

	if f == nil {
		f = &file{
			Name: n,
		}

		p.files[n] = f
	}

	return f
}

func (p *Package) readAll(h *tar.Header, r io.Reader) (_ []byte, err error) {
	if int64(int(h.Size)) != h.Size {
		return nil, errors.New("file size is out of int")
	}

	if int(h.Size) > cap(p.b) {
		n := cap(p.b)
		for n < int(h.Size) {
			n += n / 4
		}

		b := make([]byte, n)
		copy(b, p.b)
		p.b = b
	}

	read := 0

more:
	n, err := r.Read(p.b[read:h.Size])
	read += n
	tlog.Printw("read", "name", h.Name, "n", n, "read", read, "size", h.Size, "err", err)
	if err == io.EOF {
		err = nil
	}
	if err != nil {
		return nil, err
	}
	if read < int(h.Size) && n != 0 {
		goto more
	}
	if read != int(h.Size) {
		goto more
		return nil, errors.New("partial read: %d/%d", n, h.Size)
	}

	return p.b[:read], nil
}

func isUpper(c rune) bool {
	return c >= 'A' && c <= 'Z'
}
