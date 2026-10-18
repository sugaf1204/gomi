package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/clusterapi"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type ClusterAPIManager interface {
	Snapshot(context.Context) (clusterapi.Snapshot, error)
	Change(context.Context, string, string, string, clusterapi.Change) (clusterapi.Resource, error)
}

func (s *Server) ListClusterAPI(c echo.Context) error {
	if s.clusterAPI == nil {
		return c.JSON(http.StatusOK, clusterapi.Snapshot{Resources: []clusterapi.Resource{}})
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 30*time.Second)
	defer cancel()
	result, err := s.clusterAPI.Snapshot(ctx)
	if err != nil {
		return clusterAPIError(c, err)
	}
	return c.JSON(http.StatusOK, result)
}

func (s *Server) ChangeClusterAPI(c echo.Context) error {
	if s.clusterAPI == nil {
		return c.JSON(http.StatusServiceUnavailable, jsonError("Cluster API management connection is not configured"))
	}
	var change clusterapi.Change
	decoder := json.NewDecoder(http.MaxBytesReader(c.Response(), c.Request().Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&change); err != nil {
		return c.JSON(http.StatusBadRequest, jsonError("invalid Cluster API change"))
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return c.JSON(http.StatusBadRequest, jsonError("expected a single change"))
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 30*time.Second)
	defer cancel()
	namespace, resource, name := c.Param("namespace"), c.Param("resource"), c.Param("name")
	result, err := s.clusterAPI.Change(ctx, namespace, resource, name, change)
	details := map[string]string{"namespace": namespace, "resource": resource, "apiVersion": change.APIVersion}
	payload, _ := json.Marshal(change)
	details["change"] = string(payload)
	if err != nil {
		httputil.CreateAudit(c, s.authStore, "cluster-api/"+namespace+"/"+name, "cluster-api.update", "failure", err.Error(), details)
		return clusterAPIError(c, err)
	}
	httputil.CreateAudit(c, s.authStore, "cluster-api/"+namespace+"/"+name, "cluster-api.update", "success", "Desired state updated; reconciliation is asynchronous", details)
	return c.JSON(http.StatusOK, result)
}

func clusterAPIError(c echo.Context, err error) error {
	status := http.StatusBadGateway
	message := "Cluster API management request failed; check connectivity, installed CRDs and Kubernetes RBAC"
	switch {
	case errors.Is(err, clusterapi.ErrConflict), apierrors.IsConflict(err):
		status = http.StatusConflict
		message = "Resource changed. Refresh and review the change again."
	case errors.Is(err, clusterapi.ErrInvalid):
		status = http.StatusBadRequest
		message = err.Error()
	case apierrors.IsInvalid(err), apierrors.IsBadRequest(err):
		status = http.StatusBadRequest
		message = "Management cluster rejected the change. Refresh and review the resource before retrying."
	case apierrors.IsNotFound(err):
		status = http.StatusNotFound
		message = "Cluster API resource or CRD was not found"
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		status = http.StatusForbidden
		message = "Management-cluster credentials do not permit this operation"
	}
	return c.JSON(status, jsonError(message))
}
