import { useState, useEffect } from 'react'
import clsx from 'clsx'
import { api } from '../../api'
import { formatDate, phaseClass } from '../../lib/formatters'
import { usePersistentStringState } from '../../hooks/usePersistentStringState'
import { notifyError } from '../../lib/toast'
import { ModalOverlay } from '../ui/ModalOverlay'
import type { OSImage, OSImageManifest } from '../../types'

export type OSImagesViewProps = {
  osImages: OSImage[]
  onRefresh: () => void | Promise<void>
}

type OSImageForm = {
  name: string
  osFamily: string
  osVersion: string
  arch: string
  format: OSImage['format']
  variant: '' | NonNullable<OSImage['variant']>
  source: 'upload' | 'url'
  url: string
  checksum: string
  manifestText: string
  file: File | null
  windowsFiles: Partial<Record<keyof NonNullable<OSImageManifest['windows']>['winPE'], File>>
}

const initialImageForm: OSImageForm = {
  name: '',
  osFamily: 'debian',
  osVersion: '13',
  arch: 'amd64',
  format: 'qcow2',
  variant: '',
  source: 'url',
  url: '',
  checksum: '',
  manifestText: '',
  file: null,
  windowsFiles: {}
}

type ImageUploadProgress = { name: string; rootUploaded: boolean; completedArtifacts: string[] }

const OS_IMAGE_SELECTION_STORAGE_KEY = 'gomi.os-images.selected'

export function OSImagesView({ osImages, onRefresh }: OSImagesViewProps) {
  const [selectedImage, setSelectedImage] = usePersistentStringState(OS_IMAGE_SELECTION_STORAGE_KEY)
  const [imageFormOpen, setImageFormOpen] = useState(false)
  const [imageForm, setImageForm] = useState(initialImageForm)
  const [creatingImage, setCreatingImage] = useState(false)
  const [uploadingImage, setUploadingImage] = useState(false)
  const [uploadProgress, setUploadProgress] = useState<ImageUploadProgress | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<{ open: boolean; name: string }>({ open: false, name: '' })

  const fileUploadBusy = creatingImage || uploadingImage
  const metadataLocked = Boolean(uploadProgress) || fileUploadBusy
  const selectedImageData = osImages.find((img) => img.name === selectedImage) ?? null

  useEffect(() => {
    if (selectedImage && osImages.length > 0 && !osImages.some((image) => image.name === selectedImage)) {
      setSelectedImage('')
    }
  }, [selectedImage, osImages, setSelectedImage])

  useEffect(() => {
    if (!imageFormOpen) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') setImageFormOpen(false)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [imageFormOpen])

  async function handleCreateImage(e: React.FormEvent) {
    e.preventDefault()
    setCreatingImage(true)
    try {
      const manifest = parseManifestText(imageForm.manifestText)
      let progress = uploadProgress
      if (!progress) {
        const created = await api.createOSImage({
          name: imageForm.name,
          osFamily: imageForm.osFamily,
          osVersion: imageForm.osVersion,
          arch: imageForm.arch,
          format: imageForm.format,
          source: imageForm.source,
          variant: imageForm.variant || undefined,
          manifest,
          url: imageForm.source === 'url' ? imageForm.url : undefined,
          checksum: imageForm.checksum || undefined
        })
        progress = { name: created.name, rootUploaded: false, completedArtifacts: [] }
        setUploadProgress(progress)
      }
      setCreatingImage(false)
      if (imageForm.source === 'upload' && imageForm.file) {
        setUploadingImage(true)
        if (!progress.rootUploaded) {
          await api.uploadOSImageFile(progress.name, imageForm.file)
          progress = { ...progress, rootUploaded: true }
          setUploadProgress(progress)
        }
        if (manifest?.windows) {
          for (const key of ['wimboot', 'bcd', 'bootSDI', 'bootWIM'] as const) {
            const path = manifest.windows.winPE[key]
            if (progress.completedArtifacts.includes(key)) continue
            const file = imageForm.windowsFiles[key]
            if (!file) throw new Error(`Missing Windows boot asset: ${key}`)
            await api.uploadOSImageArtifact(progress.name, path, file)
            progress = { ...progress, completedArtifacts: [...progress.completedArtifacts, key] }
            setUploadProgress(progress)
          }
        }
      }
      setUploadProgress(null)
      setImageForm(initialImageForm)
      setImageFormOpen(false)
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to create OS image')
      void onRefresh()
    } finally {
      setCreatingImage(false)
      setUploadingImage(false)
    }
  }

  async function handleDeleteImage(name: string) {
    try {
      await api.deleteOSImage(name)
      if (uploadProgress?.name === name) {
        setUploadProgress(null)
        setImageForm(initialImageForm)
      }
      if (selectedImage === name) setSelectedImage('')
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to delete OS image')
    }
  }

  function readyBadge(ready: boolean) {
    return ready ? phaseClass('ready') : phaseClass('error')
  }

  return (
    <>
      {imageFormOpen && (
        <ModalOverlay onBackdropClick={() => { setImageFormOpen(false) }}>
          <div className="w-[min(520px,100%)] bg-panel border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem] max-h-[90vh] overflow-auto">
            <div className="flex justify-between items-center">
              <h3 className="text-[1.2rem]">Create OS Image</h3>
              <button
                aria-label="Close"
                className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
                onClick={() => { setImageFormOpen(false) }}
              >×</button>
            </div>
            <form className="grid gap-[0.55rem]" onSubmit={(e) => void handleCreateImage(e)}>
              {uploadProgress && <p className="m-0 text-[0.8rem] text-ink-soft">Image {uploadProgress.name} is registered. Retry resumes remaining uploads. Image metadata and completed files stay locked; pending files can be replaced; closing this dialog keeps progress for this session.</p>}
              <label className="text-[0.84rem]">
                Name
                <input required disabled={metadataLocked} value={imageForm.name} onChange={(e) => setImageForm((f) => ({ ...f, name: e.target.value }))} placeholder="e.g. debian-13-amd64-baremetal" />
              </label>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-[0.45rem]">
                <label className="text-[0.84rem]">
                  OS Family
                  <input required disabled={metadataLocked} value={imageForm.osFamily} onChange={(e) => setImageForm((f) => ({ ...f, osFamily: e.target.value, ...(e.target.value.trim().toLowerCase() === 'windows' ? { source: 'upload' as const } : {}) }))} placeholder="debian" />
                </label>
                <label className="text-[0.84rem]">
                  Version
                  <input required disabled={metadataLocked} value={imageForm.osVersion} onChange={(e) => setImageForm((f) => ({ ...f, osVersion: e.target.value }))} placeholder="24.04" />
                </label>
                <label className="text-[0.84rem]">
                  Arch
                  <select disabled={metadataLocked} value={imageForm.arch} onChange={(e) => setImageForm((f) => ({ ...f, arch: e.target.value }))}>
                    <option value="amd64">amd64</option>
                    <option value="arm64">arm64</option>
                  </select>
                </label>
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.45rem]">
                <label className="text-[0.84rem]">
                  Format
                  <select disabled={metadataLocked} value={imageForm.format} onChange={(e) => setImageForm((f) => ({ ...f, format: e.target.value as OSImage['format'], ...(e.target.value === 'wim' ? { source: 'upload' as const, osFamily: 'windows', arch: 'amd64' } : {}) }))}>
                    <option value="qcow2">qcow2</option>
                    <option value="squashfs">SquashFS</option>
                    <option value="wim">Windows WIM</option>
                  </select>
                </label>
                <label className="text-[0.84rem]">
                  Variant
                  <select disabled={metadataLocked} value={imageForm.variant} onChange={(e) => setImageForm((f) => ({ ...f, variant: e.target.value as OSImageForm['variant'] }))}>
                    <option value="">Default</option>
                    <option value="cloud">Cloud VM</option>
                    <option value="baremetal">Bare Metal</option>
                    <option value="server">Server</option>
                    <option value="desktop">Desktop</option>
                  </select>
                </label>
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.45rem]">
                <label className="text-[0.84rem]">
                  Source
                  <select disabled={metadataLocked || imageForm.format === 'wim' || imageForm.osFamily.trim().toLowerCase() === 'windows'} value={imageForm.source} onChange={(e) => setImageForm((f) => ({ ...f, source: e.target.value as 'upload' | 'url' }))}>
                    <option value="url">URL</option>
                    <option value="upload">Upload</option>
                  </select>
                </label>
              </div>

              {imageForm.source === 'url' && (
                <label className="text-[0.84rem]">
                  URL
                  <input required disabled={metadataLocked} value={imageForm.url} onChange={(e) => setImageForm((f) => ({ ...f, url: e.target.value }))} placeholder="https://example.com/os-image.qcow2" />
                </label>
              )}

              {imageForm.source === 'upload' && (
                <label className="text-[0.84rem]">
                  File
                  <input
                    type="file"
                    disabled={fileUploadBusy || Boolean(uploadProgress?.rootUploaded)}
                    required={!imageForm.file}
                    accept=".qcow2,.img,.squashfs,.wim"
                    onChange={(e) => setImageForm((f) => ({ ...f, file: e.target.files?.[0] ?? null }))}
                    className="block mt-[0.3rem] text-[0.84rem]"
                  />
                </label>
              )}

              {imageForm.source === 'upload' && imageForm.format === 'wim' && (['wimboot', 'bcd', 'bootSDI', 'bootWIM'] as const).map((key) => (
                <label key={key} className="text-[0.84rem]">WinPE {key}
                  <input type="file" disabled={fileUploadBusy || Boolean(uploadProgress?.completedArtifacts.includes(key))} required={!imageForm.windowsFiles[key]} onChange={(e) => setImageForm((f) => ({ ...f, windowsFiles: { ...f.windowsFiles, [key]: e.target.files?.[0] } }))} />
                </label>
              ))}

              {imageForm.source !== 'upload' && (
                <label className="text-[0.84rem]">
                  Checksum (SHA-256) <span className="text-ink-soft">(optional)</span>
                  <input disabled={metadataLocked} value={imageForm.checksum} onChange={(e) => setImageForm((f) => ({ ...f, checksum: e.target.value }))} placeholder="sha256:..." />
                </label>
              )}

              {imageForm.format === 'wim' && <p className="m-0 text-[0.8rem] text-ink-soft">Use OS family windows, architecture amd64 and UEFI. Upload install.wim and all four WinPE boot assets. Set windows.imageIndex and windows.winPE paths in the manifest. Product keys are entered on Windows after deployment.</p>}
              <label className="text-[0.84rem]">
                Manifest JSON <span className="text-ink-soft">(required for Windows WIM and bare-metal qcow2 artifacts)</span>
                <textarea
                  disabled={metadataLocked} value={imageForm.manifestText}
                  onChange={(e) => setImageForm((f) => ({ ...f, manifestText: e.target.value }))}
                  rows={7}
                  placeholder={imageForm.format === 'wim' ? JSON.stringify({bootModes: ['uefi'], capabilities: {deployTargets: ['vm', 'baremetal']}, root: {format: 'wim', path: 'install.wim'}, windows: {imageIndex: 1, winPE: {wimboot: 'wimboot', bcd: 'BCD', bootSDI: 'boot.sdi', bootWIM: 'boot.wim'}}}) : '{"capabilities":{"deployTargets":["baremetal"]},"root":{"format":"qcow2","path":"root.qcow2","rootPartition":{"number":1},"efiPartition":{"number":15}}}'}
                />
              </label>

              <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
                <button type="button" onClick={() => { setImageFormOpen(false) }}>Cancel</button>
                <button type="submit" disabled={creatingImage || uploadingImage} className="bg-brand border-brand-strong text-white">
                  {uploadingImage ? 'Uploading...' : creatingImage ? 'Creating...' : uploadProgress ? 'Retry uploads' : imageForm.source === 'upload' ? 'Create & Upload' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </ModalOverlay>
      )}

      <section className="min-h-0 grid grid-cols-1 md:grid-cols-[310px_minmax(0,1fr)] gap-[0.9rem]">
        <div className="min-h-0 grid grid-rows-[auto_minmax(0,1fr)] gap-[0.6rem]">
          <div className="grid gap-[0.45rem]">
            <div className="flex justify-between items-center gap-2">
              <h2 className="text-[1.4rem]">OS Images</h2>
              <button
                className="bg-brand border-brand-strong text-white py-[0.35rem] px-[0.55rem] text-[0.82rem]"
                onClick={() => { setImageFormOpen(true) }}
              >
                Create
              </button>
            </div>
          </div>

          <div className="overflow-auto border-t border-line pr-[0.1rem]">
            {osImages.map((img) => (
              <button
                key={img.name}
                className={clsx(
                  'text-left w-full border-0 border-b border-line border-l-[3px] border-l-transparent bg-transparent flex justify-between items-start gap-[0.75rem] py-[0.62rem] pl-[0.55rem] pr-[0.1rem] shadow-none hover:transform-none!',
                  selectedImage === img.name && '!border-l-brand bg-brand-wash shadow-none!'
                )}
                onClick={() => setSelectedImage(img.name)}
              >
                <div>
                  <p className="m-0 font-ui font-medium tracking-normal">{img.name}</p>
                  <p className="m-0 text-ink-soft text-[0.82rem]">{img.osFamily} {img.osVersion}{img.variant ? ` ${img.variant}` : ''} - {img.format}</p>
                </div>
                <span className={readyBadge(img.ready)}>{img.ready ? 'Ready' : 'Not Ready'}</span>
              </button>
            ))}
            {osImages.length === 0 && <p className="m-0 text-ink-soft">No OS images found</p>}
          </div>
        </div>

        <div className="min-h-0 grid content-start gap-[0.85rem]">
          {!selectedImageData && (
            <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] grid gap-[0.45rem]">
              <h2>Select an OS image</h2>
              <p>Choose an image from the list to view details.</p>
            </section>
          )}

          {selectedImageData && (
            <>
              <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem] flex justify-between items-start gap-4">
                <div>
                  <p className="m-0 font-ui font-medium text-[0.72rem] uppercase tracking-[0.08em] text-ink-soft">OS Image</p>
                  <h2 className="mt-[0.22rem] text-[1.8rem]">{selectedImageData.name}</h2>
                  <p className="m-0 text-ink-soft">{selectedImageData.osFamily} {selectedImageData.osVersion}{selectedImageData.variant ? ` ${selectedImageData.variant}` : ''} - {selectedImageData.arch}</p>
                </div>
                <div className="flex flex-col items-end gap-[0.55rem]">
                  <span className={readyBadge(selectedImageData.ready)}>{selectedImageData.ready ? 'Ready' : 'Not Ready'}</span>
                  <button
                    className="bg-danger-line border-danger-line text-white py-[0.45rem] px-[0.72rem]"
                    onClick={() => setDeleteConfirm({ open: true, name: selectedImageData.name })}
                  >
                    Delete
                  </button>
                </div>
              </section>

              <section className="grid grid-cols-1 lg:grid-cols-2 gap-[0.85rem]">
                <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                  <h3 className="mb-[0.58rem] text-[1.05rem]">Image Details</h3>
                  <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                    <dt className="text-ink-soft text-[0.84rem]">OS Family</dt><dd className="m-0">{selectedImageData.osFamily}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Version</dt><dd className="m-0">{selectedImageData.osVersion}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Arch</dt><dd className="m-0">{selectedImageData.arch}</dd>
                    <dt className="text-ink-soft text-[0.84rem]">Format</dt><dd className="m-0">{selectedImageData.format}</dd>
                    {selectedImageData.variant && (
                      <><dt className="text-ink-soft text-[0.84rem]">Variant</dt><dd className="m-0">{selectedImageData.variant}</dd></>
                    )}
                    <dt className="text-ink-soft text-[0.84rem]">Source</dt><dd className="m-0">{selectedImageData.source}</dd>
                    {selectedImageData.sizeBytes != null && (
                      <><dt className="text-ink-soft text-[0.84rem]">Size</dt><dd className="m-0">{formatSize(selectedImageData.sizeBytes)}</dd></>
                    )}
                  </dl>
                </article>

                <article className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                  <h3 className="mb-[0.58rem] text-[1.05rem]">Source Details</h3>
                  <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                    {selectedImageData.url && (
                      <><dt className="text-ink-soft text-[0.84rem]">URL</dt><dd className="m-0 break-all"><code className="text-[0.82rem]">{selectedImageData.url}</code></dd></>
                    )}
                    {selectedImageData.checksum && (
                      <><dt className="text-ink-soft text-[0.84rem]">Checksum</dt><dd className="m-0 break-all"><code className="text-[0.78rem]">{selectedImageData.checksum}</code></dd></>
                    )}
                  </dl>
                </article>
              </section>

              <section className="bg-transparent border-0 border-t border-line shadow-none pt-[0.85rem]">
                <h3 className="mb-[0.58rem] text-[1.05rem]">Status</h3>
                <dl className="m-0 grid grid-cols-[130px_minmax(0,1fr)] gap-x-[0.65rem] gap-y-[0.34rem]">
                  <dt className="text-ink-soft text-[0.84rem]">Ready</dt><dd className="m-0">{selectedImageData.ready ? 'Yes' : 'No'}</dd>
                  {selectedImageData.localPath && (
                    <><dt className="text-ink-soft text-[0.84rem]">Local Path</dt><dd className="m-0"><code className="text-[0.82rem]">{selectedImageData.localPath}</code></dd></>
                  )}
                  {selectedImageData.error && (
                    <><dt className="text-ink-soft text-[0.84rem]">Error</dt><dd className="m-0 text-error">{selectedImageData.error}</dd></>
                  )}
                  <dt className="text-ink-soft text-[0.84rem]">Created</dt><dd className="m-0">{formatDate(selectedImageData.createdAt)}</dd>
                  <dt className="text-ink-soft text-[0.84rem]">Updated</dt><dd className="m-0">{formatDate(selectedImageData.updatedAt)}</dd>
                </dl>
              </section>
            </>
          )}
        </div>
      </section>

      {deleteConfirm.open && (
        <ModalOverlay onBackdropClick={() => setDeleteConfirm({ open: false, name: '' })}>
          <div className="w-[min(400px,100%)] bg-panel border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[0.95rem] grid gap-[0.6rem]">
            <h3 className="text-[1.2rem] text-danger">Delete OS Image</h3>
            <p className="m-0 text-ink-soft text-[0.84rem]">Are you sure you want to delete this OS image?</p>
            <div className="border border-line bg-panel-2 p-[0.55rem]">
              <code>{deleteConfirm.name}</code>
            </div>
            <div className="flex justify-end gap-[0.45rem]">
              <button onClick={() => setDeleteConfirm({ open: false, name: '' })}>Cancel</button>
              <button
                className="bg-danger-line border-danger-line text-white"
                onClick={() => { void handleDeleteImage(deleteConfirm.name); setDeleteConfirm({ open: false, name: '' }) }}
              >
                Delete
              </button>
            </div>
          </div>
        </ModalOverlay>
      )}
    </>
  )
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

function parseManifestText(value: string): OSImageManifest | undefined {
  const trimmed = value.trim()
  if (!trimmed) return undefined
  const parsed = JSON.parse(trimmed) as OSImageManifest
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('Manifest JSON must be an object')
  }
  return parsed
}
