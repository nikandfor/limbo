package main

import (
	"context"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"

	"github.com/gin-gonic/gin"
	"github.com/nikandfor/cli"
	"github.com/nikandfor/tlog"
	"github.com/nikandfor/tlog/ext/tlflag"
	"github.com/nikandfor/tlog/ext/tlgin"
	"github.com/pkg/errors"

	"github.com/rndcenter/limbo"
	"github.com/rndcenter/limbo/deb"
)

func main() {
	cli.App = cli.Command{
		Name:      "limbo",
		Before:    before,
		EnvPrefix: "LIMBO_",
		Flags: []*cli.Flag{
			cli.NewFlag("path,repo", "repo", "repo root path for local storage"),
			cli.NewFlag("log", "stderr", "log destination"),
			cli.NewFlag("v", "", "verbosity"),
			cli.NewFlag("debug", "", "debug address"),
			cli.FlagfileFlag,
			cli.HelpFlag,
		},
		Commands: []*cli.Command{{
			Name:   "run",
			Action: run,
			Flags: []*cli.Flag{
				cli.NewFlag("listen,l", ":80", "address to listen to"),
			},
		}, {
			Name:   "reindex",
			Action: reindex,
		}, {
			Name:   "deb",
			Action: debdump,
			Args:   cli.Args{},
			Commands: []*cli.Command{{
				Name:   "repack",
				Action: debrepack,
				Args:   cli.Args{},
			}},
		}, {
			Name:   "q",
			Action: q,
		}},
	}

	cli.RunAndExit(os.Args)
}

func before(c *cli.Command) error {
	w, err := tlflag.ParseDestination(c.String("log"))
	if err != nil {
		return errors.Wrap(err, "parse log flag")
	}

	tlog.DefaultLogger = tlog.New(w)

	tlog.SetFilter(c.String("v"))

	ls := tlog.FillLabelsWithDefaults("_hostname", "_runid")
	tlog.SetLabels(ls)

	if a := c.String("debug"); a != "" {
		go func() {
			err := http.ListenAndServe(a, nil)
			tlog.Printw("debug server", "err", err, tlog.KeyLogLevel, tlog.Fatal)
			os.Exit(1)
		}()

		tlog.Printw("listen debug server", "addr", a)
	}

	gin.SetMode(gin.ReleaseMode)

	return nil
}

func run(c *cli.Command) error {
	tlog.Printf("os.Args: %q", os.Args)

	pth := c.String("path")

	ctx := context.Background()
	ctx = tlog.ContextWithLogger(ctx, tlog.DefaultLogger)

	lim, err := limbo.New(ctx, pth)
	if err != nil {
		return errors.Wrap(err, "open limbo")
	}

	err = lim.UpdateIndex()
	if err != nil {
		return errors.Wrap(err, "update limbo index")
	}

	r := gin.New()

	r.Use(tlgin.Tracer)

	dr := r.Group("/v0/deb/")

	dr.StaticFS("pool", http.Dir(lim.Pool))

	l, err := net.Listen("tcp", c.String("listen"))
	if err != nil {
		return errors.Wrap(err, "listen")
	}

	tlog.Printw("listening", "addr", l.Addr())

	err = http.Serve(l, r)

	tlog.Printw("serve", "err", err)

	return err
}

func reindex(c *cli.Command) error {
	pth := c.String("path")

	ctx := context.Background()
	ctx = tlog.ContextWithLogger(ctx, tlog.DefaultLogger)

	lim, err := limbo.New(ctx, pth)
	if err != nil {
		return errors.Wrap(err, "open limbo")
	}

	err = lim.UpdateIndex()
	if err != nil {
		return errors.Wrap(err, "update limbo index")
	}

	return nil
}

func debdump(c *cli.Context) error {
	if c.Args.Len() != 1 {
		return errors.New("argument expected")
	}

	ctx := context.Background()
	ctx = tlog.ContextWithLogger(ctx, tlog.DefaultLogger)

	p, err := deb.Open(ctx, c.Args.First())

	tlog.V("pkg").Printw("package", "p", p)

	return err
}

func debrepack(c *cli.Context) error {
	if c.Args.Len() != 2 {
		return errors.New("arguments expected")
	}

	ctx := context.Background()
	ctx = tlog.ContextWithLogger(ctx, tlog.DefaultLogger)

	p, err := deb.Open(ctx, c.Args.First())
	if err != nil {
		return errors.Wrap(err, "open")
	}

	err = p.Save(c.Args[1])
	if err != nil {
		return errors.Wrap(err, "save")
	}

	return nil
}

func q(c *cli.Context) error {
	for _, q := range []string{"qwe", "./qwe", "/qwe"} {
		tlog.Printw("clean", "res", path.Clean(q), "was", q)
	}

	return nil
}
