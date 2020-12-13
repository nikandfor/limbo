package deb

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

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
		Section       string   `textproto:",omitempty" json:",omitempty"`
		Priority      string   `textproto:",omitempty" json:",omitempty"`
		Maintainer    string   `textproto:",omitempty" json:",omitempty"`
		Vendor        string   `textproto:",omitempty" json:",omitempty"`
		Depends       []string `textproto:",omitempty" json:",omitempty"`
		PreDepends    []string `textproto:",omitempty" json:",omitempty"`
		Recommends    []string `textproto:",omitempty" json:",omitempty"`
		Homepage      string   `textproto:",omitempty" json:",omitempty"`
		Description   string   `textproto:",omitempty" json:",omitempty"`
		Replaces      []string `textproto:",omitempty" json:",omitempty"`
		Provides      []string `textproto:",omitempty" json:",omitempty"`
		Conflicts     []string `textproto:",omitempty" json:",omitempty"`

		Rest map[string]interface{} `textproto:",rest" json:"rest,omitempty"`
	}

	Package struct {
		Control      Control
		RestControls map[string]interface{}

		MD5Sum    [md5.Size]byte
		SHA1Sum   [sha1.Size]byte
		SHA256Sum [sha256.Size]byte

		files  map[string]*file
		filesl []*file `tlog:""`

		md5sums bool

		b, b2 bytes.Buffer

		tr tlog.Span
	}

	file struct {
		Typeflag byte
		Name     string
		Mode     int64
		ModTime  time.Time

		MD5sum [md5.Size]byte `tlog:",hex"`

		data []byte
	}

	rawControl struct {
		fs   map[string]*rawField
		rest *rawField
	}

	rawField struct {
		I         int
		Name      string
		OmitEmpty bool

		Rest bool
	}

	counter struct {
		n *int64
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

		tg := strings.Split(f.Tag.Get("textproto"), ",")

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
				switch t {
				case "omitempty":
					rf.OmitEmpty = true
				case "rest":
					rf.Rest = true
				}
			}
		}

		if rf.Rest {
			rcontrol.rest = &rf

			continue
		}

		rcontrol.fs[rf.Name] = &rf
	}
}

func New(ctx context.Context) (p *Package) {
	tr := tlog.SpawnOrStartFromContext(ctx, "deb_package", "format", "deb")

	p = &Package{
		files: make(map[string]*file),
		tr:    tr,
	}

	return p
}

func Open(ctx context.Context, fn string) (p *Package, err error) {
	p = New(ctx)
	err = p.Open(fn)
	return p, err
}

func (p *Package) Open(fn string) (err error) {
	p.tr.Printw("open", "basename", filepath.Base(fn), "file", fn)

	f, err := os.Open(fn)
	if err != nil {
		return errors.Wrap(err, "open file")
	}
	defer func() {
		e := f.Close()
		if err == nil {
			err = e
		}
	}()

	_, err = p.ReadFrom(f)

	return
}

func (p *Package) ReadFrom(r io.Reader) (n int64, err error) {
	r, sum := p.readHash(r)

	err = p.readAr(r)
	if err != nil {
		return 0, err
	}

	n, err = sum()
	if err != nil {
		return n, errors.Wrap(err, "calc hash sums")
	}

	p.b.Reset()

	p.tr.Printw("read from reader", "package", p.Control.Package, "version", p.Control.Version, "arch", p.Control.Architecture)

	return n, nil
}

func (p *Package) readHash(r io.Reader) (io.Reader, func() (int64, error)) {
	md5h := md5.New()
	sha1h := sha1.New()
	sha256h := sha256.New()

	var n int64

	w := io.MultiWriter(md5h, sha1h, sha256h, counter{&n})

	r = io.TeeReader(r, w)

	return r, func() (_ int64, err error) {
		tail, err := io.Copy(ioutil.Discard, r)
		if err != nil {
			return n, errors.Wrap(err, "read file to the end")
		}

		if tail != 0 {
			p.tr.Printw("unused file content", "len", tail)
		}

		p.tr.V("counter").Printw("bytes read", "n", n)

		_ = md5h.Sum(p.MD5Sum[:0])
		_ = sha1h.Sum(p.SHA1Sum[:0])
		_ = sha256h.Sum(p.SHA256Sum[:0])

		p.tr.Printw("file hash", "md5sum", tlog.FormatNext("%x"), p.MD5Sum)
		p.tr.Printw("file hash", "sha1sum", tlog.FormatNext("%x"), p.SHA1Sum)
		p.tr.Printw("file hash", "sha256sum", tlog.FormatNext("%x"), p.SHA256Sum)

		return n, nil
	}
}

func (p *Package) readAr(f io.Reader) (err error) {
	a := ar.NewReader(f)

	h, err := a.Next()
	if err != nil {
		return errors.Wrap(err, "read deb version (header)")
	}

	if path.Clean(h.Name) != "debian-binary" {
		return errors.New("bad deb format: expected debian-binary got %s", h.Name)
	}

	_, err = p.b.ReadFrom(a)
	if err != nil {
		return errors.Wrap(err, "read deb version (content)")
	}

	if !bytes.Equal(p.b.Bytes(), []byte("2.0\n")) && !bytes.Equal(p.b.Bytes(), []byte("2.0")) {
		return errors.New("unsupported version: %q", p.b.Bytes())
	}

	h, err = a.Next()
	if err != nil {
		return errors.Wrap(err, "read deb control: header")
	}

	if !strings.HasPrefix(h.Name, "control.") {
		return errors.New("bad deb format: expected control.tar got %s", h.Name)
	}

	err = p.readTar(h, a, p.readControl)
	if err != nil {
		return errors.Wrap(err, "read control")
	}

	h, err = a.Next()
	if err != nil {
		return errors.Wrap(err, "read deb data: header")
	}

	if !strings.HasPrefix(h.Name, "data.") {
		return errors.New("bad deb format: expected data.tar got %s", h.Name)
	}

	err = p.readTar(h, a, p.readFsys)
	if err != nil {
		return errors.Wrap(err, "read fsys")
	}

	for {
		h, err = a.Next()
		if err == io.EOF {
			return nil
		}

		p.tr.Printw("unused file in ar", "type", "", "size", h.Size, "name", h.Name)
	}

	return nil
}

func (p *Package) readTar(h *ar.Header, r io.Reader, f func(h *tar.Header, r io.Reader) error) (err error) {
	var a *tar.Reader

	p.tr.V("fileheader").Printw("read ar", "type", "", "size", h.Size, "name", h.Name)

	n := path.Clean(h.Name)

again:
	switch ext := path.Ext(n); ext {
	case ".tar", "":
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
		return errors.New("unsupported file format: %q", ext)
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
	p.tr.V("fileheader").Printw("control file", "type", tlog.FormatNext("%c"), h.Typeflag, "size", h.Size, "name", h.Name)

	if h.Typeflag != tar.TypeReg {
		p.tr.Printw("skip control: not regular", "type", tlog.FormatNext("%c"), h.Typeflag)
		// skip
		return nil
	}

	if p.tr.If("filecontent") {
		data, err := p.readAll(h, r)
		if err != nil {
			return errors.Wrap(err, "read file content: %v", h.Name)
		}

		p.tr.Printf("file content:\n%s", data)

		r = bytes.NewReader(data)
	}

	name := path.Clean(h.Name)

	switch name {
	case "control":
		_, err = p.Control.ReadFrom(r)
	case "md5sums":
		p.md5sums = true
		err = p.parseMD5Sums(r)
	default:
		err = p.readRestControl(name, r)
	}

	err = errors.Wrap(err, "parse %v", h.Name)

	return err
}

func (c *Control) ReadFrom(r io.Reader) (n int64, err error) {
	if c == nil {
		return 0, errors.New("nil Control")
	}

	v := reflect.ValueOf(c).Elem()

	r = io.TeeReader(r, counter{&n})

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
				return n, errors.Wrap(err, "parse %s", name)
			}

			f.SetInt(q)
		case reflect.Slice:
			fvs := strings.Split(string(val), ",")
			for i := range fvs {
				fvs[i] = strings.TrimSpace(fvs[i])
			}

			f.Set(reflect.ValueOf(fvs))
		default:
			return n, errors.New("unsupported field type: %v", f.Type())
		}
	}

	return n, nil
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

func (p *Package) readFsys(h *tar.Header, r io.Reader) error {
	p.tr.V("fileheader").Printw("fsys file", "type", tlog.FormatNext("%c"), h.Typeflag, "size", h.Size, "name", h.Name)

	data, err := ioutil.ReadAll(r)
	if err != nil {
		return errors.Wrap(err, "read file content")
	}

	var f *file

	if p.md5sums && h.Typeflag == tar.TypeReg {
		f = p.files[path.Clean(h.Name)]
		if f == nil {
			//	return errors.New("%v: no md5sum", h.Name)
			p.tr.Printw("no md5sum", "file", h.Name)
		}

		md5sum := md5.Sum(data)
		if f != nil && f.MD5sum != md5sum {
			//	return errors.New("%v: md5sum mismatch", h.Name)

			p.tr.Printw("md5sum mismatch", "file", h.Name, "md5sum", md5sum, "debmd5", f.MD5sum)
		}
	}
	if f == nil {
		f = p.file(h.Name)
	}

	f.Typeflag = h.Typeflag
	f.Mode = h.Mode
	f.ModTime = h.ModTime

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

	p.b.Reset()
	_, err = p.b.ReadFrom(r)
	if err != nil {
		return nil, errors.Wrap(err, "read file content")
	}

	return p.b.Bytes(), nil
}

func isUpper(c rune) bool {
	return c >= 'A' && c <= 'Z'
}

func (w counter) Write(p []byte) (n int, err error) {
	*w.n += int64(len(p))

	return len(p), nil
}
