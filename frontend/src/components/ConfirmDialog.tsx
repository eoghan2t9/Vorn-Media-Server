import { useEffect } from 'react'
import { XIcon } from './icons'
import './Modal.css'

export function ConfirmDialog({
  title,
  message,
  confirmLabel = 'Confirm',
  danger = false,
  busy = false,
  onConfirm,
  onCancel,
}: {
  title: string
  message: string
  confirmLabel?: string
  danger?: boolean
  busy?: boolean
  onConfirm: () => void
  onCancel: () => void
}) {
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onCancel()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [onCancel])

  return (
    <div className="vorn-modal-overlay" onClick={onCancel}>
      <div className="vorn-modal" onClick={(e) => e.stopPropagation()}>
        <div className="vorn-modal-header">
          <h2>{title}</h2>
          <button type="button" className="vorn-modal-close" onClick={onCancel} aria-label="Close">
            <XIcon />
          </button>
        </div>
        <div className="vorn-modal-body">
          <p>{message}</p>
        </div>
        <div className="vorn-modal-footer">
          <button type="button" onClick={onCancel} disabled={busy}>
            Cancel
          </button>
          <button type="button" className={danger ? 'vorn-btn-danger' : 'vorn-btn-primary'} onClick={onConfirm} disabled={busy}>
            {busy ? 'Working…' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
