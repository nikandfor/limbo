package limbo

import (
	"context"
	"os"
	"path/filepath"

	"github.com/nikandfor/tlog"
	"github.com/pkg/errors"
	"github.com/rndcenter/limbo/deb"
)

type (
	Limbo struct {
		Path string
		Pool string

		ctx context.Context
		tr  tlog.Span
	}
)

func New(ctx context.Context, p string) (*Limbo, error) {
	tr := tlog.SpawnOrStartFromContext(ctx, "limbo")

	l := &Limbo{
		Path: p,
		Pool: filepath.Join(p, "pool"),

		ctx: context.Background(),
		tr:  tr,
	}

	if tr.Logger != nil {
		l.ctx = tlog.ContextWithSpan(l.ctx, tr)
	}

	return l, nil
}

func (l *Limbo) UpdateIndex() (err error) {
	err = os.MkdirAll(l.Pool, 0755)
	if err != nil {
		return errors.Wrap(err, "create pool dir")
	}

	l.tr.Printw("read pool")

	err = filepath.Walk(l.Pool, func(path string, inf os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if inf.IsDir() {
			return nil
		}

		ext := filepath.Ext(path)
		if ext != ".deb" {
			return nil
		}

		//	tr := l.tr.Spawn("pool_file")
		//	ctx := tlog.ContextWithSpan(context.Background(), tr)

		p, err := deb.Open(l.ctx, path)
		if err != nil {
			l.tr.Printw("index pkg pool", "path", path, "err", tlog.FormatNext("%+v"), err)
			return nil
		}

		_ = p

		return nil
	})
	if err != nil {
		return errors.Wrap(err, "read pool")
	}

	return nil
}
