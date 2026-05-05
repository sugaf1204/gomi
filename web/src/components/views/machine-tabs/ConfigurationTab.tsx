import clsx from 'clsx'
import { formatPowerControlMethod, powerMethodLabel } from '../../../lib/formatters'
import type { Machine, PowerConfig, PowerType } from '../../../types'

type Props = {
  machine: Machine
  onSaveMachineSettings: () => void | Promise<void>
  machineSettingsDirty: boolean
  machineSettingsSaving: boolean
  inlineEditField: 'power' | null
  onInlineEditFieldChange: (field: 'power' | null) => void
  machineSettingsPower: PowerConfig
  onMachineSettingsPowerChange: (value: PowerConfig) => void
}

function PowerConnectionDetails({ power }: { power: PowerConfig }) {
  if (power.type === 'ipmi' && power.ipmi) {
    return (
      <dl className="m-0 grid grid-cols-[160px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.28rem] max-sm:grid-cols-[130px_minmax(0,1fr)] text-[0.84rem]">
        <dt className="text-ink-soft">BMC Host</dt><dd className="m-0">{power.ipmi.host}</dd>
        <dt className="text-ink-soft">Username</dt><dd className="m-0">{power.ipmi.username}</dd>
        {power.ipmi.interface && <><dt className="text-ink-soft">Interface</dt><dd className="m-0">{power.ipmi.interface}</dd></>}
      </dl>
    )
  }
  if (power.type === 'webhook' && power.webhook) {
    return (
      <dl className="m-0 grid grid-cols-[160px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.28rem] max-sm:grid-cols-[130px_minmax(0,1fr)] text-[0.84rem]">
        <dt className="text-ink-soft">Power On URL</dt><dd className="m-0 break-all">{power.webhook.powerOnURL}</dd>
        <dt className="text-ink-soft">Power Off URL</dt><dd className="m-0 break-all">{power.webhook.powerOffURL}</dd>
        {power.webhook.statusURL && <><dt className="text-ink-soft">Status URL</dt><dd className="m-0 break-all">{power.webhook.statusURL}</dd></>}
        {power.webhook.bootOrderURL && <><dt className="text-ink-soft">Boot Order URL</dt><dd className="m-0 break-all">{power.webhook.bootOrderURL}</dd></>}
      </dl>
    )
  }
  if (power.type === 'wol' && power.wol) {
    return (
      <dl className="m-0 grid grid-cols-[160px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.28rem] max-sm:grid-cols-[130px_minmax(0,1fr)] text-[0.84rem]">
        <dt className="text-ink-soft">Wake MAC</dt><dd className="m-0">{power.wol.wakeMAC}</dd>
        {power.wol.broadcastIP && <><dt className="text-ink-soft">Broadcast IP</dt><dd className="m-0">{power.wol.broadcastIP}{power.wol.port ? `:${power.wol.port}` : ''}</dd></>}
        {power.wol.shutdownTarget && <><dt className="text-ink-soft">Shutdown Target</dt><dd className="m-0">{power.wol.shutdownTarget}{power.wol.shutdownUDPPort ? `:${power.wol.shutdownUDPPort}` : ''}</dd></>}
      </dl>
    )
  }
  return null
}

export function ConfigurationTab({
  machine,
  onSaveMachineSettings,
  machineSettingsDirty,
  machineSettingsSaving,
  inlineEditField,
  onInlineEditFieldChange,
  machineSettingsPower,
  onMachineSettingsPowerChange,
}: Props) {
  function handleTypeChange(newType: PowerType) {
    switch (newType) {
      case 'ipmi':
        onMachineSettingsPowerChange({ type: 'ipmi', ipmi: { host: '', username: '', password: '' } })
        break
      case 'webhook':
        onMachineSettingsPowerChange({ type: 'webhook', webhook: { powerOnURL: '', powerOffURL: '' } })
        break
      case 'wol':
        onMachineSettingsPowerChange({ type: 'wol', wol: { wakeMAC: machine.mac } })
        break
      case 'manual':
      default:
        onMachineSettingsPowerChange({ type: 'manual' })
        break
    }
  }

  return (
    <div className="pt-[0.65rem]">
      <div className="flex justify-between items-center gap-[0.8rem] mb-[0.58rem]">
        <div className="flex items-center gap-[0.55rem]">
          <h3 className="text-[1.05rem]">Power Control</h3>
          {machineSettingsDirty && (
            <span className="inline-flex items-center font-ui rounded-full text-[0.68rem] font-semibold px-2 py-0.5 bg-[#f8e6cc] text-warn">Unsaved changes</span>
          )}
        </div>
        <button
          className={clsx(
            'min-w-[94px]',
            machineSettingsDirty && !machineSettingsSaving && 'bg-brand border-brand-strong text-white'
          )}
          onClick={() => void onSaveMachineSettings()}
          disabled={machineSettingsSaving || !machineSettingsDirty}
        >
          {machineSettingsSaving ? 'Saving...' : 'Save'}
        </button>
      </div>

      <dl className="m-0 grid grid-cols-[160px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem] max-sm:grid-cols-[130px_minmax(0,1fr)]">
        <dt className="text-ink-soft text-[0.84rem]">Method</dt>
        <dd className="m-0">
          <span className="inline-flex items-center font-ui rounded-full text-[0.71rem] font-semibold px-2 py-0.5 bg-[#d9f1e5] text-ok">
            {powerMethodLabel(machineSettingsPower.type)}
          </span>
        </dd>

        <dt className="text-ink-soft text-[0.84rem]">Power Type</dt>
        <dd className="m-0">
          {inlineEditField === 'power' ? (
            <div className="grid gap-[0.45rem]">
              <select
                className="w-[min(280px,100%)] font-ui text-[0.9rem] py-[0.42rem] px-[0.55rem]"
                value={machineSettingsPower.type}
                onChange={(e) => handleTypeChange(e.target.value as PowerType)}
              >
                <option value="manual">Manual</option>
                <option value="ipmi">IPMI</option>
                <option value="webhook">Webhook</option>
                <option value="wol">Wake-on-LAN</option>
              </select>
              {machineSettingsPower.type === 'ipmi' && (
                <div className="grid gap-[0.35rem] pl-[0.4rem] border-l-2 border-brand/30">
                  <input
                    placeholder="BMC Host"
                    value={machineSettingsPower.ipmi?.host ?? ''}
                    onChange={(e) => onMachineSettingsPowerChange({ ...machineSettingsPower, ipmi: { ...machineSettingsPower.ipmi!, host: e.target.value } })}
                  />
                  <input
                    placeholder="Username"
                    value={machineSettingsPower.ipmi?.username ?? ''}
                    onChange={(e) => onMachineSettingsPowerChange({ ...machineSettingsPower, ipmi: { ...machineSettingsPower.ipmi!, username: e.target.value } })}
                  />
                  <input
                    type="password"
                    placeholder="Password"
                    value={machineSettingsPower.ipmi?.password ?? ''}
                    onChange={(e) => onMachineSettingsPowerChange({ ...machineSettingsPower, ipmi: { ...machineSettingsPower.ipmi!, password: e.target.value } })}
                  />
                </div>
              )}
              {machineSettingsPower.type === 'webhook' && (
                <div className="grid gap-[0.35rem] pl-[0.4rem] border-l-2 border-brand/30">
                  <input
                    placeholder="Power On URL"
                    value={machineSettingsPower.webhook?.powerOnURL ?? ''}
                    onChange={(e) => onMachineSettingsPowerChange({ ...machineSettingsPower, webhook: { ...machineSettingsPower.webhook!, powerOnURL: e.target.value } })}
                  />
                  <input
                    placeholder="Power Off URL"
                    value={machineSettingsPower.webhook?.powerOffURL ?? ''}
                    onChange={(e) => onMachineSettingsPowerChange({ ...machineSettingsPower, webhook: { ...machineSettingsPower.webhook!, powerOffURL: e.target.value } })}
                  />
                  <input
                    placeholder="Status URL (optional)"
                    value={machineSettingsPower.webhook?.statusURL ?? ''}
                    onChange={(e) => onMachineSettingsPowerChange({ ...machineSettingsPower, webhook: { ...machineSettingsPower.webhook!, statusURL: e.target.value } })}
                  />
                  <input
                    placeholder="Boot Order URL (optional, BIOS deploy)"
                    value={machineSettingsPower.webhook?.bootOrderURL ?? ''}
                    onChange={(e) => onMachineSettingsPowerChange({ ...machineSettingsPower, webhook: { ...machineSettingsPower.webhook!, bootOrderURL: e.target.value } })}
                  />
                </div>
              )}
              {machineSettingsPower.type === 'wol' && (
                <div className="grid gap-[0.35rem] pl-[0.4rem] border-l-2 border-brand/30">
                  <input
                    placeholder="Wake MAC"
                    value={machineSettingsPower.wol?.wakeMAC ?? ''}
                    onChange={(e) => onMachineSettingsPowerChange({ ...machineSettingsPower, wol: { ...machineSettingsPower.wol!, wakeMAC: e.target.value } })}
                  />
                </div>
              )}
              <button
                type="button"
                className="w-fit text-[0.82rem] py-[0.3rem] px-[0.5rem]"
                onClick={() => onInlineEditFieldChange(null)}
              >
                Done
              </button>
            </div>
          ) : (
            <button
              type="button"
              className="border-0 bg-transparent p-0 text-ink text-left shadow-none font-semibold hover:transform-none! hover:shadow-none! hover:underline disabled:opacity-100! disabled:text-ink disabled:cursor-default disabled:no-underline"
              onClick={() => onInlineEditFieldChange('power')}
              disabled={machineSettingsSaving}
              title="Click to edit power configuration"
            >
              {formatPowerControlMethod(machineSettingsPower)}
            </button>
          )}
        </dd>
      </dl>

      {!inlineEditField && (machineSettingsPower.type === 'ipmi' || machineSettingsPower.type === 'webhook' || machineSettingsPower.type === 'wol') && (
        <div className="mt-[0.7rem] pt-[0.55rem] border-t border-line">
          <h4 className="text-[0.88rem] text-ink-soft mb-[0.38rem]">Connection Details</h4>
          <PowerConnectionDetails power={machineSettingsPower} />
        </div>
      )}
    </div>
  )
}
