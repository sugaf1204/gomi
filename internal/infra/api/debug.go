package api

import (
	gohttp "net/http"
	"net/http/pprof"

	"github.com/felixge/fgprof"
	"github.com/labstack/echo/v4"
)

func (s *Server) registerDebugRoutes() {
	debug := s.echo.Group("/debug")

	debug.GET("/pprof", echo.WrapHandler(gohttp.HandlerFunc(pprof.Index)))
	debug.GET("/pprof/", echo.WrapHandler(gohttp.HandlerFunc(pprof.Index)))
	debug.GET("/pprof/cmdline", echo.WrapHandler(gohttp.HandlerFunc(pprof.Cmdline)))
	debug.GET("/pprof/profile", echo.WrapHandler(gohttp.HandlerFunc(pprof.Profile)))
	debug.GET("/pprof/symbol", echo.WrapHandler(gohttp.HandlerFunc(pprof.Symbol)))
	debug.GET("/pprof/trace", echo.WrapHandler(gohttp.HandlerFunc(pprof.Trace)))
	debug.GET("/pprof/:profile", echo.WrapHandler(gohttp.HandlerFunc(pprof.Index)))
	debug.GET("/fgprof", echo.WrapHandler(fgprof.Handler()))
}
