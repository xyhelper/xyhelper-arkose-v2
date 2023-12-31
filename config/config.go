package config

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcache"
	"github.com/gogf/gf/v2/os/gctx"
)

var (
	PORT    = 8080
	PROXY   = ""
	Cache   = gcache.New()
	AUTHKEY = g.Cfg().MustGetWithEnv(gctx.GetInitCtx(), "AUTHKEY").String()
)

func init() {
	ctx := gctx.GetInitCtx()
	port := g.Cfg().MustGetWithEnv(ctx, "PORT").Int()
	if port > 0 {
		PORT = port
	}
	proxy := g.Cfg().MustGetWithEnv(ctx, "PROXY").String()
	if proxy != "" {
		PROXY = proxy
	}
}
