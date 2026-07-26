import { useMemo, useState } from 'react'
import type { CloudInitTemplate, Hypervisor, OSImage, SSHKey, Subnet } from '../../types'
import { supportsDeploymentTarget } from '../../lib/osImages'
import { VMConfigFields } from '../views/virtual-machines/VMConfigFields'
import { VMQuickDeployDialog } from '../views/virtual-machines/VMQuickDeployDialog'
import { buildBridgePlaceholder } from '../views/virtual-machines/vmFormState'
import type { UpdateVMConfigForm } from '../views/virtual-machines/vmFormState'
import type { VMQuickDeploy } from '../../hooks/useVMQuickDeploy'

export type VMQuickDeploySettingsProps = {
  quickDeploy: VMQuickDeploy
  hypervisors: Hypervisor[]
  osImages: OSImage[]
  cloudInits: CloudInitTemplate[]
  sshKeys: SSHKey[]
  subnets: Subnet[]
  onRefresh: () => void | Promise<void>
}

// Wires the shared VM config fields into the Quick Deploy dialog. Lives in
// layout/ because the dialog is opened from the workspace header rather than
// from the Virtual Machines view.
export function VMQuickDeploySettings({
  quickDeploy,
  hypervisors,
  osImages,
  cloudInits,
  sshKeys,
  subnets,
  onRefresh
}: VMQuickDeploySettingsProps) {
  const [advancedOpen, setAdvancedOpen] = useState(false)

  const osImageByName = useMemo(() => new Map(osImages.map((img) => [img.name, img])), [osImages])
  const vmOSImages = useMemo(() => osImages.filter((img) => supportsDeploymentTarget(img, 'vm')), [osImages])
  const bridgePlaceholder = useMemo(() => buildBridgePlaceholder(hypervisors, subnets), [hypervisors, subnets])

  const updateForm: UpdateVMConfigForm = (updater) => {
    quickDeploy.setPreset((current) => ({
      ...current,
      ...updater(current)
    }))
  }

  return (
    <VMQuickDeployDialog
      open={quickDeploy.settingsOpen}
      preset={quickDeploy.preset}
      setPreset={quickDeploy.setPreset}
      quickDeploying={quickDeploy.deploying}
      nextName={quickDeploy.nextVMName}
      isReady={quickDeploy.presetReady}
      onClose={quickDeploy.closeSettings}
      onDeploy={() => void quickDeploy.deploy()}
    >
      <VMConfigFields
        formState={quickDeploy.preset}
        updateForm={updateForm}
        advancedExpanded={advancedOpen}
        setAdvancedExpanded={setAdvancedOpen}
        radioNamePrefix="quick-deploy"
        hypervisors={hypervisors}
        vmOSImages={vmOSImages}
        cloudInits={cloudInits}
        sshKeys={sshKeys}
        subnets={subnets}
        osImageByName={osImageByName}
        onRefresh={onRefresh}
        bridgePlaceholder={bridgePlaceholder}
        supportsTemplateNameHostname
      />
    </VMQuickDeployDialog>
  )
}
