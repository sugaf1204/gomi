package libvirt

import (
	"context"
	"fmt"

	golibvirt "github.com/digitalocean/go-libvirt"
)

func (e *rpcExecutor) DefineDomain(_ context.Context, cfg DomainConfig) error {
	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		return fmt.Errorf("generate domain xml: %w", err)
	}

	_, err = e.l.DomainDefineXMLFlags(xmlStr, 0)
	if err != nil {
		return fmt.Errorf("define domain: %w", err)
	}
	return nil
}

func (e *rpcExecutor) StartDomain(_ context.Context, name string) error {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", name, err)
	}
	if err := e.l.DomainCreate(domain); err != nil {
		return fmt.Errorf("start domain %s: %w", name, err)
	}
	return nil
}

func (e *rpcExecutor) ShutdownDomain(_ context.Context, name string) error {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", name, err)
	}
	if err := e.l.DomainShutdown(domain); err != nil {
		return fmt.Errorf("shutdown domain %s: %w", name, err)
	}
	return nil
}

func (e *rpcExecutor) DestroyDomain(_ context.Context, name string) error {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", name, err)
	}
	if err := e.l.DomainDestroy(domain); err != nil {
		return fmt.Errorf("destroy domain %s: %w", name, err)
	}
	return nil
}

func (e *rpcExecutor) UndefineDomain(_ context.Context, name string) error {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", name, err)
	}
	if err := e.l.DomainUndefineFlags(domain, golibvirt.DomainUndefineManagedSave|golibvirt.DomainUndefineSnapshotsMetadata); err != nil {
		return fmt.Errorf("undefine domain %s: %w", name, err)
	}
	return nil
}

func (e *rpcExecutor) SetDomainBootDevice(_ context.Context, name string, bootDev string) error {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", name, err)
	}
	xmlDesc, err := e.l.DomainGetXMLDesc(domain, 0)
	if err != nil {
		return fmt.Errorf("get domain xml %s: %w", name, err)
	}
	updatedXML, err := rewriteDomainBootDeviceXML(xmlDesc, bootDev)
	if err != nil {
		return fmt.Errorf("rewrite boot device %s: %w", name, err)
	}
	if _, err := e.l.DomainDefineXMLFlags(updatedXML, 0); err != nil {
		return fmt.Errorf("define domain with boot device %s: %w", name, err)
	}
	return nil
}

func (e *rpcExecutor) MigrateDomain(_ context.Context, name string, destURI string, flags golibvirt.DomainMigrateFlags) error {
	domain, err := e.l.DomainLookupByName(name)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", name, err)
	}
	if _, err := e.l.DomainMigratePerform3Params(
		domain,
		golibvirt.OptString{destURI},
		nil, // params
		nil, // cookie
		flags,
	); err != nil {
		return fmt.Errorf("migrate domain %s to %s: %w", name, destURI, err)
	}
	return nil
}
