module github.com/rndcenter/limbo

go 1.15

require (
	github.com/blakesmith/ar v0.0.0-20190502131153-809d4375e1fb
	github.com/gin-gonic/gin v1.6.3
	github.com/nikandfor/cli v0.0.0-20201116184530-576a69d47ee7
	github.com/nikandfor/errors v0.3.1-0.20201212142705-56fda2c0e8b3
	github.com/nikandfor/loc v0.0.0-20201209201630-39582039abc5
	github.com/nikandfor/tlog v0.9.1-0.20201112213439-20db076f10c2
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.6.1
	github.com/ulikunitz/xz v0.5.8
	github.com/xi2/xz v0.0.0-20171230120015-48954b6210f8
)

replace github.com/nikandfor/tlog => ../../nikandfor/tlog

replace github.com/nikandfor/cli => ../../nikandfor/cli
