import { useEffect, useState } from 'react'
import { ApiError, browseFilesystem, type BrowseEntry } from '../api/client'
import { FolderIcon, XIcon } from './icons'
import './Modal.css'
import './DirectoryBrowser.css'

export function DirectoryBrowser({
  initialPath,
  onClose,
  onSelect,
}: {
  initialPath?: string
  onClose: () => void
  onSelect: (path: string) => void
}) {
  const [path, setPath] = useState(initialPath ?? '')
  const [parent, setParent] = useState<string | undefined>(undefined)
  const [dirs, setDirs] = useState<BrowseEntry[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    browseFilesystem(path || undefined)
      .then((res) => {
        if (cancelled) return
        setPath(res.path)
        setParent(res.parent)
        setDirs(res.directories)
      })
      .catch((err) => !cancelled && setError(err instanceof ApiError ? err.message : String(err)))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
    // Only re-fetch when navigating (path changes below via setPath from a
    // click) -- `path` itself is the trigger, so it's the only dependency.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path])

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [onClose])

  return (
    <div className="vorn-modal-overlay" onClick={onClose}>
      <div className="vorn-modal vorn-directory-browser" onClick={(e) => e.stopPropagation()}>
        <div className="vorn-modal-header">
          <h2>Choose a folder</h2>
          <button type="button" className="vorn-modal-close" onClick={onClose} aria-label="Close">
            <XIcon />
          </button>
        </div>

        <div className="vorn-directory-path">{path || '/'}</div>

        <div className="vorn-directory-list">
          {error && <p className="vorn-form-error">{error}</p>}
          {!error && loading && <p className="vorn-empty">Loading…</p>}
          {!error && !loading && (
            <>
              {parent !== undefined && (
                <button type="button" className="vorn-directory-entry" onClick={() => setPath(parent)}>
                  <FolderIcon className="vorn-directory-icon" />
                  <span>.. (up)</span>
                </button>
              )}
              {dirs.length === 0 && parent === undefined && <p className="vorn-empty">No subfolders here.</p>}
              {dirs.map((d) => (
                <button type="button" key={d.path} className="vorn-directory-entry" onClick={() => setPath(d.path)}>
                  <FolderIcon className="vorn-directory-icon" />
                  <span>{d.name}</span>
                </button>
              ))}
            </>
          )}
        </div>

        <div className="vorn-modal-footer">
          <button type="button" onClick={onClose}>
            Cancel
          </button>
          <button type="button" className="vorn-btn-primary" disabled={!path} onClick={() => onSelect(path)}>
            Use this folder
          </button>
        </div>
      </div>
    </div>
  )
}
