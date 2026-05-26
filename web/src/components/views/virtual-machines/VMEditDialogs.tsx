import type { Dispatch, FormEvent, ReactNode, SetStateAction } from 'react'
import { ModalOverlay } from '../../ui/ModalOverlay'
import type { VirtualMachine } from '../../../types'
import type { UpdateVMConfigForm, VMForm, VMReinstallForm } from './vmFormState'

type VMEditDialogsProps = {
  formOpen: boolean
  creating: boolean
  form: VMForm
  setForm: Dispatch<SetStateAction<VMForm>>
  advancedOpen: boolean
  setAdvancedOpen: Dispatch<SetStateAction<boolean>>
  onCloseCreate: () => void
  onCreate: (event: FormEvent) => void
  reinstallOpen: boolean
  selectedVM: VirtualMachine | null
  reinstalling: boolean
  reinstallForm: VMReinstallForm
  updateReinstallConfigForm: UpdateVMConfigForm
  reinstallAdvancedOpen: boolean
  setReinstallAdvancedOpen: Dispatch<SetStateAction<boolean>>
  onCloseReinstall: () => void
  onRedeploy: () => void
  renderFields: (
    form: VMForm | VMReinstallForm,
    updateForm: UpdateVMConfigForm,
    advancedOpen: boolean,
    setAdvancedOpen: Dispatch<SetStateAction<boolean>>,
    radioName: string
  ) => ReactNode
}

export function VMEditDialogs({
  formOpen,
  creating,
  form,
  setForm,
  advancedOpen,
  setAdvancedOpen,
  onCloseCreate,
  onCreate,
  reinstallOpen,
  selectedVM,
  reinstalling,
  reinstallForm,
  updateReinstallConfigForm,
  reinstallAdvancedOpen,
  setReinstallAdvancedOpen,
  onCloseReinstall,
  onRedeploy,
  renderFields
}: VMEditDialogsProps) {
  return (
    <>
      {formOpen && (
        <ModalOverlay onBackdropClick={() => { if (!creating) onCloseCreate() }}>
          <div className="w-[min(760px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem] max-h-[90vh] overflow-auto">
            <div className="flex justify-between items-center">
              <h3 className="text-[1.2rem]">Create Virtual Machine</h3>
              <button aria-label="Close" className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!" disabled={creating} onClick={onCloseCreate}>x</button>
            </div>
            <form className="grid gap-[0.55rem]" onSubmit={onCreate}>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.55rem]">
                <label className="text-[0.84rem] min-w-0">
                  Name
                  <input required value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} placeholder="e.g. vm-web-01" />
                </label>
                <label className="text-[0.84rem] min-w-0">
                  Count
                  <input type="number" min="1" max="50" value={form.count} onChange={(e) => setForm((f) => ({ ...f, count: e.target.value }))} />
                </label>
              </div>
              {Number(form.count) > 1 && form.name && (
                <p className="m-0 text-ink-soft text-[0.82rem]">
                  Will create: {Array.from({ length: Math.min(Number(form.count) || 1, 5) }, (_, i) => `${form.name}-${i + 1}`).join(', ')}
                  {Number(form.count) > 5 && `, ... (${form.count} total)`}
                </p>
              )}
              {renderFields(form, (updater) => setForm((current) => ({ ...current, ...updater(current) })), advancedOpen, setAdvancedOpen, 'vm-create')}
              <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
                <button type="button" onClick={onCloseCreate} disabled={creating}>Cancel</button>
                <button
                  type="submit"
                  disabled={creating || !form.name.trim() || !form.osImageRef || (form.ipAssignment === 'static' && !form.staticIP.trim()) || (form.cloudInitMode === 'create' && (!form.cloudInitTemplateName.trim() || !form.cloudInitUserData.trim()))}
                  className="bg-brand border-brand-strong text-white"
                >
                  {creating ? 'Creating...' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </ModalOverlay>
      )}

      {reinstallOpen && selectedVM && (
        <ModalOverlay onBackdropClick={() => { if (!reinstalling) onCloseReinstall() }}>
          <div className="w-[min(760px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem] max-h-[90vh] overflow-auto">
            <div className="flex justify-between items-center">
              <h3 className="text-[1.2rem]">Redeploy VM: {selectedVM.name}</h3>
              <button aria-label="Close" className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!" disabled={reinstalling} onClick={onCloseReinstall}>x</button>
            </div>
            <p className="m-0 text-ink-soft text-[0.84rem]">Current VM spec is pre-filled. You can redeploy as-is or update values before execution.</p>
            <div className="grid gap-[0.55rem]">
              {renderFields(reinstallForm, updateReinstallConfigForm, reinstallAdvancedOpen, setReinstallAdvancedOpen, 'vm-redeploy')}
              <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
                <button type="button" onClick={onCloseReinstall}>Cancel</button>
                <button
                  type="button"
                  disabled={reinstalling || !reinstallForm.osImageRef || (reinstallForm.ipAssignment === 'static' && !reinstallForm.staticIP.trim()) || (reinstallForm.cloudInitMode === 'create' && (!reinstallForm.cloudInitTemplateName.trim() || !reinstallForm.cloudInitUserData.trim()))}
                  className="bg-brand border-brand-strong text-white"
                  onClick={onRedeploy}
                >
                  {reinstalling ? 'Redeploying...' : 'Redeploy'}
                </button>
              </div>
            </div>
          </div>
        </ModalOverlay>
      )}
    </>
  )
}
