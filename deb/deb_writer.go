package deb

import (
	"archive/tar"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/blakesmith/ar"
	"github.com/nikandfor/errors"
	"github.com/nikandfor/tlog"

	"github.com/rndcenter/limbo/textproto"
)

func (p *Package) CanonicalName() string {
	return p.Control.Package + "_" + p.Control.Version + "_" + p.Control.Architecture + ".deb"
}

func (p *Package) Save(fn string) (err error) {
	p.tr.Printw("create file", "basename", filepath.Base(fn), "file", fn)

	f, err := os.Create(fn)
	if err != nil {
		return errors.Wrap(err, "create file")
	}
	defer func() {
		e := f.Close()
		if err == nil {
			err = e
		}
	}()

	_, err = p.WriteTo(f)

	return
}

func (p *Package) WriteTo(w io.Writer) (n int64, err error) {
	p.tr.Printw("write to writer", "package", p.Control.Package, "version", p.Control.Version, "arch", p.Control.Architecture)

	w, sum := p.writeHash(w)

	err = p.writeAr(w)
	if err != nil {
		return 0, err
	}

	n, err = sum()
	if err != nil {
		return n, errors.Wrap(err, "calc hash sums")
	}

	p.b.Reset()

	return
}

func (p *Package) writeHash(w io.Writer) (io.Writer, func() (n int64, err error)) {
	md5h := md5.New()
	sha1h := sha1.New()
	sha256h := sha256.New()

	var n int64

	w = io.MultiWriter(w, md5h, sha1h, sha256h, counter{&n})

	return w, func() (_ int64, err error) {
		p.tr.V("counter").Printw("bytes written", "n", n)

		_ = md5h.Sum(p.MD5Sum[:0])
		_ = sha1h.Sum(p.SHA1Sum[:0])
		_ = sha256h.Sum(p.SHA256Sum[:0])

		p.tr.Printw("file hash", "md5sum", tlog.FormatNext("%x"), p.MD5Sum)
		p.tr.Printw("file hash", "sha1sum", tlog.FormatNext("%x"), p.SHA1Sum)
		p.tr.Printw("file hash", "sha256sum", tlog.FormatNext("%x"), p.SHA256Sum)

		return n, nil
	}
}

func (p *Package) writeAr(w io.Writer) (err error) {
	a := ar.NewWriter(w)

	err = a.WriteGlobalHeader()
	if err != nil {
		return errors.Wrap(err, "write global header")
	}

	h := ar.Header{
		Name: "debian-binary",
		Size: 4,
	}

	err = a.WriteHeader(&h)
	if err != nil {
		return errors.Wrap(err, "write deb version (header)")
	}

	_, err = a.Write([]byte("2.0\n"))
	if err != nil {
		return errors.Wrap(err, "write deb version (content)")
	}

	err = p.writeTar(a, "control.tar", p.writeControl)
	if err != nil {
		return errors.Wrap(err, "write control")
	}

	err = p.writeTar(a, "data.tar", p.writeFsys)
	if err != nil {
		return errors.Wrap(err, "write data")
	}

	return nil
}

func (p *Package) writeTar(w *ar.Writer, n string, f func(w *tar.Writer) error) (err error) {
	p.b.Reset()

	a := tar.NewWriter(&p.b)

	err = f(a)
	if err != nil {
		return err
	}

	err = a.Close()
	if err != nil {
		return errors.Wrap(err, "close tar")
	}

	h := ar.Header{
		Name:    n,
		ModTime: time.Now(),
		Mode:    0544,
		Size:    int64(p.b.Len()),
	}

	err = w.WriteHeader(&h)
	if err != nil {
		return errors.Wrap(err, "write ar header")
	}

	m, err := w.Write(p.b.Bytes())
	if err != nil {
		return errors.Wrap(err, "write ar data")
	}

	if m != int(h.Size+h.Size&1) {
		return errors.New("bad written: %v != %v", m, h.Size)
	}

	return nil
}

func (p *Package) writeControl(w *tar.Writer) (err error) {
	now := time.Now()

	err = p.writeControlControl(w, now)
	if err != nil {
		return errors.Wrap(err, "write control control")
	}

	for name, data := range p.RestControls {
		if name == "" {
			return errors.New("empty rest control name")
		}

		p.b2.Reset()

		switch d := data.(type) {
		case []byte:
			_, _ = p.b2.Write(d)
		case string:
			_, _ = p.b2.WriteString(d)
		default:
			return errors.New("not supported yet")
		}

		h := tar.Header{
			Typeflag: tar.TypeReg,
			Name:     name,
			Mode:     0544,
			ModTime:  now,
			Size:     int64(p.b2.Len()),
		}

		err = w.WriteHeader(&h)
		if err != nil {
			return errors.Wrap(err, "write %v header", name)
		}

		_, err := w.Write(p.b2.Bytes())
		//	p.tr.Printw("tar file written", "n", n, "size", h.Size, "err", err)
		if err != nil {
			return errors.Wrap(err, "write %v data", name)
		}
	}

	return nil
}

func (p *Package) writeControlControl(w *tar.Writer, now time.Time) (err error) {
	p.b2.Reset()

	_, err = p.Control.WriteTo(&p.b2)
	if err != nil {
		return errors.Wrap(err, "encode")
	}

	p.tr.V("filecontent").Printf("control\n%s", p.b2.Bytes())

	h := tar.Header{
		Typeflag: tar.TypeReg,
		Name:     "control",
		Mode:     0544,
		ModTime:  now,
		Size:     int64(p.b2.Len()),
	}

	err = w.WriteHeader(&h)
	if err != nil {
		return errors.Wrap(err, "write header")
	}

	_, err = p.b2.WriteTo(w)
	if err != nil {
		return errors.Wrap(err, "write data")
	}

	return err
}

func (c *Control) WriteTo(w io.Writer) (n int64, err error) {
	tw := textproto.NewWriter(io.MultiWriter(
		w,
		counter{&n},
	))

	r := reflect.ValueOf(c).Elem()

	for _, f := range rcontrol.fs {
		fv := r.Field(f.I)
		if f.OmitEmpty && fv.IsZero() {
			continue
		}

		err = tw.KeyString(f.Name)
		if err != nil {
			return
		}

		switch v := fv.Interface().(type) {
		case string:
			err = tw.ValueString(v)
		case []string:
			l := strings.Join(v, ", ")

			err = tw.ValueString(l)
		case int64:
			err = tw.ValueString(strconv.FormatInt(v, 10))
		default:
			err = errors.New("unsupported type: %T", v)
		}
		if err != nil {
			return
		}
	}

	return n, nil
}

func (p *Package) writeFsys(w *tar.Writer) (err error) {
	for _, f := range p.filesl {
		h := tar.Header{
			Typeflag: f.Typeflag,
			Name:     f.Name,
			Mode:     f.Mode,
			ModTime:  f.ModTime,
			Size:     int64(len(f.data)),
		}

		err = w.WriteHeader(&h)
		if err != nil {
			return errors.Wrap(err, "write %v header", f.Name)
		}

		_, err = w.Write(f.data)
		if err != nil {
			return errors.Wrap(err, "write %v data", f.Name)
		}
	}

	return nil
}
