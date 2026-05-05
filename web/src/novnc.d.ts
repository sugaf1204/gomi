declare module '@novnc/novnc' {
  export default class RFB {
    constructor(target: HTMLElement, url: string, options?: {
      shared?: boolean
      credentials?: { username?: string; password?: string; target?: string }
      repeaterID?: string
      wsProtocols?: string[]
    })

    viewOnly: boolean
    focusOnClick: boolean
    clipViewport: boolean
    dragViewport: boolean
    scaleViewport: boolean
    resizeSession: boolean
    showDotCursor: boolean
    qualityLevel: number
    compressionLevel: number

    disconnect(): void
    sendCredentials(credentials: { username?: string; password?: string; target?: string }): void
    sendKey(keysym: number, code: string | null, down?: boolean): void
    sendCtrlAltDel(): void
    focus(): void
    blur(): void

    addEventListener(type: 'connect', listener: (e: CustomEvent) => void): void
    addEventListener(type: 'disconnect', listener: (e: CustomEvent<{ clean: boolean }>) => void): void
    addEventListener(type: 'credentialsrequired', listener: (e: CustomEvent) => void): void
    addEventListener(type: 'securityfailure', listener: (e: CustomEvent<{ status: number; reason: string }>) => void): void
    addEventListener(type: 'clipboard', listener: (e: CustomEvent<{ text: string }>) => void): void
    addEventListener(type: 'bell', listener: (e: CustomEvent) => void): void
    addEventListener(type: 'desktopname', listener: (e: CustomEvent<{ name: string }>) => void): void
    addEventListener(type: 'capabilities', listener: (e: CustomEvent) => void): void
    removeEventListener(type: string, listener: EventListener): void
  }
}
