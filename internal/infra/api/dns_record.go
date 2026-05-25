package api

import (
	gohttp "net/http"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/infra/dns"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
)

func (s *Server) ListDNSRecords(c echo.Context) error {
	if s.dnsRecords == nil {
		return c.JSON(gohttp.StatusConflict, jsonError("embedded DNS mode is required"))
	}
	records, err := s.dnsRecords.ListDynamicRecords(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, itemsResponse[dns.DynamicRecord]{Items: records})
}

func (s *Server) CreateDNSRecord(c echo.Context) error {
	if s.dnsRecords == nil {
		return c.JSON(gohttp.StatusConflict, jsonError("embedded DNS mode is required"))
	}
	var record dns.DynamicRecord
	if err := c.Bind(&record); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	created, err := s.dnsRecords.UpsertDynamicRecord(c.Request().Context(), record)
	if err != nil {
		return c.JSON(dnsRecordErrorStatus(err), jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, "", "create-dns-record", "success", "dns record created: "+created.Name+" "+created.Type, nil)
	return c.JSON(gohttp.StatusCreated, created)
}

func (s *Server) UpdateDNSRecord(c echo.Context) error {
	if s.dnsRecords == nil {
		return c.JSON(gohttp.StatusConflict, jsonError("embedded DNS mode is required"))
	}
	var record dns.DynamicRecord
	if err := c.Bind(&record); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	record.Name = c.Param("name")
	record.Type = c.Param("type")
	updated, err := s.dnsRecords.UpsertDynamicRecord(c.Request().Context(), record)
	if err != nil {
		return c.JSON(dnsRecordErrorStatus(err), jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, "", "update-dns-record", "success", "dns record updated: "+updated.Name+" "+updated.Type, nil)
	return c.JSON(gohttp.StatusOK, updated)
}

func (s *Server) DeleteDNSRecord(c echo.Context) error {
	if s.dnsRecords == nil {
		return c.JSON(gohttp.StatusConflict, jsonError("embedded DNS mode is required"))
	}
	name := c.Param("name")
	recordType := c.Param("type")
	if err := s.dnsRecords.DeleteDynamicRecord(c.Request().Context(), name, recordType); err != nil {
		return c.JSON(dnsRecordErrorStatus(err), jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, "", "delete-dns-record", "success", "dns record deleted: "+name+" "+recordType, nil)
	return c.NoContent(gohttp.StatusNoContent)
}

func dnsRecordErrorStatus(err error) int {
	if dns.IsDynamicRecordValidationError(err) {
		return gohttp.StatusBadRequest
	}
	return gohttp.StatusInternalServerError
}
