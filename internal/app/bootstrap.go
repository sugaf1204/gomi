package app

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/infra/netdetect"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/subnet"
)

func (r *Runtime) bootstrap(ctx context.Context) error {
	if err := r.ensureBootstrapAdmin(ctx, r.Config.AdminUsername, r.Config.AdminPassword); err != nil {
		return err
	}
	if err := r.ensureDefaultResources(ctx); err != nil {
		return err
	}
	r.migratePowerPolicyRefToInline(ctx)
	return nil
}

func (r *Runtime) ensureBootstrapAdmin(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" && password == "" {
		log.Printf("admin bootstrap disabled: create the first admin from the setup UI or API")
		return nil
	}
	if username == "" || password == "" {
		return errors.New("admin bootstrap requires both username and password")
	}
	_, err := r.authStore.GetUser(ctx, username)
	if err == nil {
		return nil
	}
	if !errors.Is(err, resource.ErrNotFound) {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return r.authStore.UpsertUser(ctx, auth.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         auth.RoleAdmin,
		CreatedAt:    time.Now().UTC(),
	})
}

func (r *Runtime) ensureDefaultResources(ctx context.Context) error {
	now := time.Now().UTC()

	if _, err := r.subnetStore.Get(ctx, "default"); err != nil {
		if !errors.Is(err, resource.ErrNotFound) {
			return err
		}
		spec := r.loadSubnetSpec()
		sub := subnet.Subnet{
			Name:      "default",
			Spec:      spec,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := r.subnetStore.Upsert(ctx, sub); err != nil {
			return err
		}
		log.Printf("bootstrapped default subnet: %s", spec.CIDR)
	}

	return nil
}

// migratePowerPolicyRefToInline sets Power.Type to "manual" for any machine
// that has an empty Power config (e.g. machines created before migration).
func (r *Runtime) migratePowerPolicyRefToInline(ctx context.Context) {
	machines, err := r.machineStore.List(ctx)
	if err != nil {
		return
	}
	for _, m := range machines {
		if m.Power.Type != "" {
			continue
		}
		m.Power = power.PowerConfig{Type: power.PowerTypeManual}
		m.UpdatedAt = time.Now().UTC()
		if err := r.machineStore.Upsert(ctx, m); err != nil {
			log.Printf("migration: failed to set default power for %s: %v", m.Name, err)
		} else {
			log.Printf("migration: set power=manual for %s", m.Name)
		}
	}
}

func (r *Runtime) loadSubnetSpec() subnet.SubnetSpec {
	configPath := os.Getenv("GOMI_SUBNET_CONFIG")
	if configPath == "" {
		configPath = filepath.Join(r.Config.DataDir, "subnet.yaml")
	}

	data, err := os.ReadFile(configPath)
	if err == nil {
		var spec subnet.SubnetSpec
		if err := yaml.Unmarshal(data, &spec); err == nil && spec.CIDR != "" {
			log.Printf("loaded subnet config from %s", configPath)
			return spec
		}
		log.Printf("warning: failed to parse subnet config %s: %v", configPath, err)
	}

	detected, err := netdetect.Detect()
	if err != nil || detected.CIDR == "" {
		return subnet.SubnetSpec{
			CIDR:       "10.0.0.0/24",
			DNSServers: []string{"8.8.8.8"},
		}
	}

	log.Printf("auto-detected network: iface=%s cidr=%s gw=%s", detected.InterfaceName, detected.CIDR, detected.Gateway)
	return subnet.SubnetSpec{
		CIDR:             detected.CIDR,
		PXEInterface:     detected.InterfaceName,
		DefaultGateway:   detected.Gateway,
		DNSServers:       detected.DNSServers,
		DNSSearchDomains: detected.SearchDomains,
	}
}
