import { AlertTriangle } from 'lucide-react'

// SessionNotices says what a session runs with instead of what it was asked
// for, such as a reasoning effort the model did not offer: the thread would
// otherwise look like it uses the configured level while the agent runs its
// own default.
export function SessionNotices({ notices }: { notices?: string[] }) {
  const shown = (notices ?? []).filter((notice) => notice.trim() !== '')
  if (shown.length === 0) return null
  return (
    <div className="mb-2 flex flex-col gap-1.5 px-1">
      {shown.map((notice) => (
        <p key={notice} role="status" className="flex min-w-0 items-start gap-1.5 text-[12px] leading-[1.5] text-running select-text">
          <AlertTriangle size={13} className="mt-[1px] shrink-0" aria-hidden />
          <span className="min-w-0 [overflow-wrap:break-word]">{notice}</span>
        </p>
      ))}
    </div>
  )
}
