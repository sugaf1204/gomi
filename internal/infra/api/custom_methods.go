package api

import (
	gohttp "net/http"

	"github.com/labstack/echo/v4"
)

func (s *Server) DispatchMachineCustomMethod(c echo.Context) error {
	name, method, ok := splitCustomMethod(c.Param("*"))
	if !ok {
		return c.JSON(gohttp.StatusNotFound, jsonError("custom method not found"))
	}
	switch method {
	case "redeploy":
		return withNameParam(c, name, s.RedeployMachine)
	case "reinstall":
		return withNameParam(c, name, s.ReinstallMachine)
	case "powerOn":
		return withNameParam(c, name, s.PowerOnMachine)
	case "powerOff":
		return withNameParam(c, name, s.PowerOffMachine)
	default:
		return c.JSON(gohttp.StatusNotFound, jsonError("custom method not found"))
	}
}

func (s *Server) DispatchVirtualMachineCustomMethod(c echo.Context) error {
	name, method, ok := splitCustomMethod(c.Param("*"))
	if !ok {
		return c.JSON(gohttp.StatusNotFound, jsonError("custom method not found"))
	}
	switch method {
	case "redeploy":
		return withNameParam(c, name, s.RedeployVM)
	case "reinstall":
		return withNameParam(c, name, s.ReinstallVM)
	case "powerOn":
		return withNameParam(c, name, s.PowerOnVM)
	case "powerOff":
		return withNameParam(c, name, s.PowerOffVM)
	case "migrate":
		return withNameParam(c, name, s.MigrateVM)
	default:
		return c.JSON(gohttp.StatusNotFound, jsonError("custom method not found"))
	}
}
