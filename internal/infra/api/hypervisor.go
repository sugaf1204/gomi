package api

import (
	"errors"
	gohttp "net/http"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/resource"
)

func (s *Server) CreateHypervisor(c echo.Context) error {
	var h hypervisor.Hypervisor
	if err := c.Bind(&h); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	created, err := s.hypervisors.Create(c.Request().Context(), h)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, created.Name, "create-hypervisor", "success", "hypervisor created", nil)
	return c.JSON(gohttp.StatusCreated, created)
}

func (s *Server) ListHypervisors(c echo.Context) error {
	items, err := s.hypervisors.List(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, itemsResponse[hypervisor.Hypervisor]{Items: items})
}

func (s *Server) GetHypervisor(c echo.Context) error {
	name := c.Param("name")
	h, err := s.hypervisors.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, h)
}

func (s *Server) DeleteHypervisor(c echo.Context) error {
	name := c.Param("name")
	if err := s.hypervisors.Delete(c.Request().Context(), name); err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, name, "delete-hypervisor", "success", "hypervisor deleted", nil)
	return c.NoContent(gohttp.StatusNoContent)
}

func (s *Server) CreateRegistrationToken(c echo.Context) error {
	token, err := s.hypervisors.CreateToken(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, "", "create-registration-token", "success", "registration token created", nil)
	return c.JSON(gohttp.StatusCreated, token)
}

func (s *Server) RegisterHypervisor(c echo.Context) error {
	var req hypervisor.RegisterRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}

	h, agentToken, err := s.hypervisors.Register(c.Request().Context(), req)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}

	return c.JSON(gohttp.StatusCreated, hypervisor.RegisterResponse{Hypervisor: h, AgentToken: agentToken})
}

func (s *Server) CreateAgentToken(c echo.Context) error {
	name := c.Param("name")
	token, err := s.hypervisors.CreateAgentToken(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, name, "create-agent-token", "success", "agent token created", nil)
	return c.JSON(gohttp.StatusCreated, tokenResponse{Token: token})
}

func (s *Server) SetupAndRegisterScript(c echo.Context) error {
	script := `#!/bin/bash
set -euo pipefail

GOMI_SERVER="${GOMI_SERVER:?GOMI_SERVER env var is required}"
GOMI_TOKEN="${GOMI_TOKEN:?GOMI_TOKEN env var is required}"

echo "Installing libvirt and dependencies..."
apt-get update -qq
apt-get install -y -qq libvirt-daemon-system libvirt-clients qemu-system virtinst cloud-image-utils curl jq zstd

# Enable libvirtd TCP listener (port 16509)
LIBVIRT_CONF="/etc/libvirt/libvirtd.conf"
if grep -qE '^[[:space:]]*#?[[:space:]]*auth_tcp[[:space:]]*=' "${LIBVIRT_CONF}"; then
  sed -i -E 's|^[[:space:]]*#?[[:space:]]*auth_tcp[[:space:]]*=.*|auth_tcp = "none"|' "${LIBVIRT_CONF}"
else
  printf '\nauth_tcp = "none"\n' >> "${LIBVIRT_CONF}"
fi
systemctl enable libvirtd-tcp.socket
systemctl stop libvirtd.service || true
systemctl start libvirtd-tcp.socket

HOSTNAME="${GOMI_HOSTNAME:-$(hostname -f)}"
IP=$(hostname -I | awk '{print $1}')
CPU_CORES=$(nproc)
MEMORY_MB=$(awk '/MemTotal/ {printf "%d", $2/1024}' /proc/meminfo)
STORAGE_GB=$(df -BG / | awk 'NR==2 {print $2}' | tr -d 'G')

echo "Registering hypervisor ${HOSTNAME} at ${GOMI_SERVER}..."
RESPONSE=$(curl -sf -X POST "${GOMI_SERVER}/api/v1/hypervisors/register" \
  -H "Content-Type: application/json" \
  -d "{
    \"token\": \"${GOMI_TOKEN}\",
    \"hostname\": \"${HOSTNAME}\",
    \"connection\": {
      \"type\": \"tcp\",
      \"host\": \"${IP}\",
      \"port\": 16509
    },
    \"capacity\": {
      \"cpuCores\": ${CPU_CORES},
      \"memoryMB\": ${MEMORY_MB},
      \"storageGB\": ${STORAGE_GB}
    }
  }")

echo "${RESPONSE}" | jq .

AGENT_TOKEN=$(echo "${RESPONSE}" | jq -r '.agentToken')

# Install gomi-hypervisor agent
echo "Installing gomi-hypervisor agent..."
ARCH=$(uname -m)
case "${ARCH}" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  *)       echo "Unsupported architecture: ${ARCH}"; exit 1 ;;
esac
curl -sf -o /usr/bin/gomi-hypervisor "${GOMI_SERVER}/files/gomi-hypervisor-linux-${ARCH}" || echo "Warning: gomi-hypervisor binary not available, skipping agent install"
if [ -f /usr/bin/gomi-hypervisor ]; then
  chmod +x /usr/bin/gomi-hypervisor
  curl -sf -o /etc/systemd/system/gomi-hypervisor.service "${GOMI_SERVER}/files/gomi-hypervisor.service"

  mkdir -p /etc/gomi
  cat > /etc/gomi/gomi-hypervisor.conf <<AGENTEOF
GOMI_SERVER_URL=${GOMI_SERVER}
GOMI_AGENT_TOKEN=${AGENT_TOKEN}
AGENTEOF

  systemctl daemon-reload
  systemctl enable --now gomi-hypervisor.service
  echo "gomi-hypervisor agent installed and started."
fi

echo "Hypervisor registered successfully."
`
	return c.String(gohttp.StatusOK, script)
}
