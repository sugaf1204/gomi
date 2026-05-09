import { useState, useEffect } from 'react'
import clsx from 'clsx'
import { api } from '../../api'
import { formatDate, phaseClass } from '../../lib/formatters'
import { usePersistentStringState } from '../../hooks/usePersistentStringState'
import { notifyError } from '../../lib/toast'
import type { OSCatalogItem, OSImage } from '../../types'

export type OSImagesViewProps = {
  osImages: OSImage[]
  onRefresh: () => void | Promise<void>
}

type OSImageForm = {
  name: string
  osFamily: string
  osVersion: string
  arch: string
  format: 'qcow2' | 'raw' | 'iso' | 'squashfs'
  source: 'upload' | 'url'
  url: string
  checksum: string
  file: File | null
}

const initialImageForm: OSImageForm = {
  name: '',
  osFamily: 'debian',
  osVersion: '13',
  arch: 'amd64',
  format: 'raw',
  source: 'url',
  url: '',
  checksum: '',
  file: null
}

const OS_IMAGE_SELECTION_STORAGE_KEY = 'gomi.os-images.selected'

export function OSImagesView({ osImages, onRefresh }: OSImagesViewProps) {
  const [selectedImage, setSelectedImage] = usePersistentStringState(OS_IMAGE_SELECTION_STORAGE_KEY)
  const [imageFormOpen, setImageFormOpen] = useState(false)
  const [imageForm, setImageForm] = useState(initialImageForm)
  const [creatingImage, setCreatingImage] = useState(false)
  const [uploadingImage, setUploadingImage] = useState(false)
  const [catalogItems, setCatalogItems] = useState<OSCatalogItem[]>([])
  const [installingCatalog, setInstallingCatalog] = useState<string | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<{ open: boolean; name: string }>({ open: false, name: '' })

  const selectedImageData = osImages.find((img) => img.name === selectedImage) ?? null

  useEffect(() => {
    if (selectedImage && osImages.length > 0 && !osImages.some((image) => image.name === selectedImage)) {
      setSelectedImage('')
    }
  }, [selectedImage, osImages, setSelectedImage])

  useEffect(() => {
    void refreshCatalog()
  }, [])

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
      const created = await api.createOSImage({
        name: imageForm.name,
        osFamily: imageForm.osFamily,
        osVersion: imageForm.osVersion,
        arch: imageForm.arch,
        format: imageForm.format,
        source: imageForm.source,
        url: imageForm.source === 'url' ? imageForm.url : undefined,
        checksum: imageForm.checksum || undefined
      })
      setCreatingImage(false)

      if (imageForm.source === 'upload' && imageForm.file) {
        setUploadingImage(true)
        try {
          await api.uploadOSImageFile(created.name, imageForm.file)
        } catch (err) {
          notifyError(err instanceof Error ? err.message : 'Image registered but file upload failed')
          setUploadingImage(false)
          void onRefresh()
          return
        }
        setUploadingImage(false)
      }

      setImageForm(initialImageForm)
      setImageFormOpen(false)
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to create OS image')
      setCreatingImage(false)
    }
  }

  async function handleDeleteImage(name: string) {
    try {
      await api.deleteOSImage(name)
      if (selectedImage === name) setSelectedImage('')
      void onRefresh()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to delete OS image')
    }
  }

  async function refreshCatalog() {
    try {
      const response = await api.listOSCatalog()
      setCatalogItems(response.items ?? [])
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to load OS catalog')
    }
  }

  async function handleInstallCatalog(name: string) {
    setInstallingCatalog(name)
    try {
      await api.installOSCatalogEntry(name)
      await Promise.all([onRefresh(), refreshCatalog()])
    } catch (err) {
      notifyError(err instanceof Error ? err.message : 'Failed to install OS image')
    } finally {
      setInstallingCatalog(null)
    }
  }

  function readyBadge(ready: boolean) {
    return ready ? phaseClass('ready') : phaseClass('error')
  }

  function bootEnvBadge(item: OSCatalogItem) {
    if (item.bootEnvironment.phase === 'ready') return phaseClass('ready')
    if (item.bootEnvironment.phase === 'building' || item.bootEnvironment.phase === 'missing') return phaseClass('pending')
    return phaseClass('error')
  }

  function catalogActionLabel(item: OSCatalogItem) {
    if (installingCatalog === item.entry.name || item.installing) return 'Installing'
    if (item.installed) return 'Installed'
    if (item.osImageReady && item.bootEnvironment.phase === 'building') return 'Building'
    return 'Install'
  }

  function catalogActionDisabled(item: OSCatalogItem) {
    return item.installed || item.installing || item.bootEnvironment.phase === 'building' || installingCatalog !== null
  }

  return (
    <>
      {imageFormOpen && (
        <div
          className="fixed inset-0 bg-[rgba(30,28,24,0.38)] grid place-items-center z-20 p-4"
          role="dialog"
          aria-modal="true"
          onClick={(e) => { if (e.target === e.currentTarget) { setImageFormOpen(false) } }}
        >
          <div className="w-[min(520px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[1.1rem] grid gap-[0.65rem] max-h-[90vh] overflow-auto">
            <div className="flex justify-between items-center">
              <h3 className="text-[1.2rem]">Create OS Image</h3>
              <button
                aria-label="Close"
                className="border-0 bg-transparent shadow-none p-0 w-[1.8rem] h-[1.8rem] flex items-center justify-center text-[1.4rem] leading-none text-ink-soft hover:text-ink hover:shadow-none!"
                onClick={() => { setImageFormOpen(false) }}
              >×</button>
            </div>
            <form className="grid gap-[0.55rem]" onSubmit={(e) => void handleCreateImage(e)}>
              <label className="text-[0.84rem]">
                Name
                <input required value={imageForm.name} onChange={(e) => setImageForm((f) => ({ ...f, name: e.target.value }))} placeholder="e.g. debian-13-amd64-baremetal" />
              </label>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-[0.45rem]">
                <label className="text-[0.84rem]">
                  OS Family
                  <input required value={imageForm.osFamily} onChange={(e) => setImageForm((f) => ({ ...f, osFamily: e.target.value }))} placeholder="debian" />
                </label>
                <label className="text-[0.84rem]">
                  Version
                  <input required value={imageForm.osVersion} onChange={(e) => setImageForm((f) => ({ ...f, osVersion: e.target.value }))} placeholder="24.04" />
                </label>
                <label className="text-[0.84rem]">
                  Arch
                  <select value={imageForm.arch} onChange={(e) => setImageForm((f) => ({ ...f, arch: e.target.value }))}>
                    <option value="amd64">amd64</option>
                    <option value="arm64">arm64</option>
                  </select>
                </label>
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-[0.45rem]">
                <label className="text-[0.84rem]">
                  Format
                  <select value={imageForm.format} onChange={(e) => setImageForm((f) => ({ ...f, format: e.target.value as 'qcow2' | 'raw' | 'iso' | 'squashfs' }))}>
                    <option value="qcow2">qcow2</option>
                    <option value="raw">raw</option>
                    <option value="iso">iso</option>
                    <option value="squashfs">squashfs</option>
                  </select>
                </label>
                <label className="text-[0.84rem]">
                  Source
                  <select value={imageForm.source} onChange={(e) => setImageForm((f) => ({ ...f, source: e.target.value as 'upload' | 'url' }))}>
                    <option value="url">URL</option>
                    <option value="upload">Upload</option>
                  </select>
                </label>
              </div>

              {imageForm.source === 'url' && (
                <label className="text-[0.84rem]">
                  URL
                  <input required value={imageForm.url} onChange={(e) => setImageForm((f) => ({ ...f, url: e.target.value }))} placeholder="https://example.com/os-image.raw" />
                </label>
              )}

              {imageForm.source === 'upload' && (
                <label className="text-[0.84rem]">
                  File
                  <input
                    type="file"
                    required
                    accept=".qcow2,.img,.iso,.raw,.squashfs"
                    onChange={(e) => setImageForm((f) => ({ ...f, file: e.target.files?.[0] ?? null }))}
                    className="block mt-[0.3rem] text-[0.84rem]"
                  />
                </label>
              )}

              {imageForm.source !== 'upload' && (
                <label className="text-[0.84rem]">
                  Checksum (SHA-256) <span className="text-ink-soft">(optional)</span>
                  <input value={imageForm.checksum} onChange={(e) => setImageForm((f) => ({ ...f, checksum: e.target.value }))} placeholder="sha256:..." />
                </label>
              )}

              <div className="flex justify-end gap-[0.45rem] pt-[0.2rem]">
                <button type="button" onClick={() => { setImageFormOpen(false) }}>Cancel</button>
                <button type="submit" disabled={creatingImage || uploadingImage} className="bg-brand border-brand-strong text-white">
                  {uploadingImage ? 'Uploading...' : creatingImage ? 'Creating...' : imageForm.source === 'upload' ? 'Create & Upload' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      <section className="mb-[0.9rem] border-t border-line pt-[0.85rem] grid gap-[0.65rem]">
        <div className="flex items-center justify-between gap-[0.75rem]">
          <h2 className="text-[1.15rem]">Supported Images</h2>
          <button className="py-[0.34rem] px-[0.55rem] text-[0.8rem]" onClick={() => void refreshCatalog()}>
            Refresh
          </button>
        </div>
        <div className="overflow-auto">
          <table className="w-full border-collapse text-[0.86rem]">
            <thead>
              <tr className="text-left text-ink-soft border-b border-line">
                <th className="py-[0.42rem] pr-[0.75rem] font-ui font-medium">Name</th>
                <th className="py-[0.42rem] pr-[0.75rem] font-ui font-medium">OS</th>
                <th className="py-[0.42rem] pr-[0.75rem] font-ui font-medium">Boot Env</th>
                <th className="py-[0.42rem] pr-[0.75rem] font-ui font-medium">Status</th>
                <th className="py-[0.42rem] text-right font-ui font-medium">Action</th>
              </tr>
            </thead>
            <tbody>
              {catalogItems.map((item) => (
                <tr key={item.entry.name} className="border-b border-line">
                  <td className="py-[0.52rem] pr-[0.75rem] whitespace-nowrap font-ui font-medium">{item.entry.name}</td>
                  <td className="py-[0.52rem] pr-[0.75rem] whitespace-nowrap">{item.entry.osFamily} {item.entry.osVersion} / {item.entry.arch}</td>
                  <td className="py-[0.52rem] pr-[0.75rem] whitespace-nowrap">
                    <span className={bootEnvBadge(item)}>{item.bootEnvironment.phase}</span>
                  </td>
                  <td className="py-[0.52rem] pr-[0.75rem] whitespace-nowrap">
                    <span className={item.installed ? phaseClass('ready') : item.osImageError ? phaseClass('error') : phaseClass('pending')}>
                      {item.installed ? 'Installed' : item.osImageError ? 'Error' : item.installing ? 'Installing' : 'Available'}
                    </span>
                  </td>
                  <td className="py-[0.52rem] text-right">
                    <button
                      className="py-[0.34rem] px-[0.62rem] text-[0.8rem]"
                      disabled={catalogActionDisabled(item)}
                      onClick={() => void handleInstallCatalog(item.entry.name)}
                    >
                      {catalogActionLabel(item)}
                    </button>
                  </td>
                </tr>
              ))}
              {catalogItems.length === 0 && (
                <tr>
                  <td className="py-[0.55rem] text-ink-soft" colSpan={5}>No catalog entries found</td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>

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
                  selectedImage === img.name && '!border-l-brand bg-[rgba(43,122,120,0.06)] shadow-none!'
                )}
                onClick={() => setSelectedImage(img.name)}
              >
                <div>
                  <p className="m-0 font-ui font-medium tracking-normal">{img.name}</p>
                  <p className="m-0 text-ink-soft text-[0.82rem]">{img.osFamily} {img.osVersion} - {img.format}</p>
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
                  <p className="m-0 text-ink-soft">{selectedImageData.osFamily} {selectedImageData.osVersion} - {selectedImageData.arch}</p>
                </div>
                <div className="flex flex-col items-end gap-[0.55rem]">
                  <span className={readyBadge(selectedImageData.ready)}>{selectedImageData.ready ? 'Ready' : 'Not Ready'}</span>
                  <button
                    className="bg-[#d86b6b] border-[#be5252] text-white py-[0.45rem] px-[0.72rem]"
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
        <div
          className="fixed inset-0 bg-[rgba(30,28,24,0.38)] grid place-items-center z-20 p-4"
          role="dialog"
          aria-modal="true"
          onClick={(e) => { if (e.target === e.currentTarget) setDeleteConfirm({ open: false, name: '' }) }}
        >
          <div className="w-[min(400px,100%)] bg-white border border-line-strong shadow-[0_20px_45px_rgba(52,43,34,0.2)] p-[0.95rem] grid gap-[0.6rem]">
            <h3 className="text-[1.2rem] text-[#9b2d2d]">Delete OS Image</h3>
            <p className="m-0 text-ink-soft text-[0.84rem]">Are you sure you want to delete this OS image?</p>
            <div className="border border-line bg-[#f9f7f4] p-[0.55rem]">
              <code>{deleteConfirm.name}</code>
            </div>
            <div className="flex justify-end gap-[0.45rem]">
              <button onClick={() => setDeleteConfirm({ open: false, name: '' })}>Cancel</button>
              <button
                className="bg-[#d86b6b] border-[#be5252] text-white"
                onClick={() => { void handleDeleteImage(deleteConfirm.name); setDeleteConfirm({ open: false, name: '' }) }}
              >
                Delete
              </button>
            </div>
          </div>
        </div>
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
