import type { DataQualityNotice } from '../../lib/deploy-timeline'

type Props = {
  notices: DataQualityNotice[]
}

// Data-quality problems are explicit UI, never silent fallbacks: anything the
// timeline had to guess, clamp, or leave out is announced here.
export function DeployNotices({ notices }: Props) {
  if (notices.length === 0) return null
  return (
    <div className="border border-warn-line bg-warn-bg px-[0.65rem] py-[0.5rem]">
      {notices.map((notice) => (
        <p key={`${notice.kind}-${notice.text}`} className="m-0 py-[0.1rem] text-[0.78rem] leading-[1.4] text-warn">
          {notice.text}
        </p>
      ))}
    </div>
  )
}
