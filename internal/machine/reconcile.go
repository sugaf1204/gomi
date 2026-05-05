package machine

import "time"

type SyncResult struct {
	Machine   Machine
	NeedsSave bool
	NeedsDNS  bool
}

func SyncState(m Machine, artifacts map[string]string, installCfg string, buildErr error) SyncResult {
	if m.Phase != PhaseProvisioning {
		return SyncResult{Machine: m}
	}

	now := time.Now().UTC()

	if buildErr != nil {
		m.Phase = PhaseError
		m.LastError = buildErr.Error()
		if m.Provision != nil {
			m.Provision.Active = false
			m.Provision.FinishedAt = &now
			m.Provision.Message = buildErr.Error()
		}
		m.UpdatedAt = now
		return SyncResult{Machine: m, NeedsSave: true}
	}

	m.LastError = ""
	if m.Provision != nil {
		m.Provision.FinishedAt = &now
		m.Provision.Message = "provision artifacts generated"
		nextArtifacts := make(map[string]string, len(m.Provision.Artifacts)+len(artifacts)+1)
		for k, v := range m.Provision.Artifacts {
			nextArtifacts[k] = v
		}
		for k, v := range artifacts {
			nextArtifacts[k] = v
		}
		nextArtifacts["installConfig.inline"] = installCfg
		m.Provision.Artifacts = nextArtifacts
	}
	m.UpdatedAt = now

	return SyncResult{Machine: m, NeedsSave: true, NeedsDNS: m.IP != "" && m.Network.Domain != ""}
}
