import { useEffect, useRef, useState } from 'react'
import {
  ApiError,
  autoBackupDownloadUrl,
  backupDownloadUrl,
  deleteAutoBackup,
  fetchBackupSettings,
  listAutoBackups,
  restoreAutoBackup,
  restoreBackup,
  updateBackupSettings,
  type AutoBackup,
  type BackupSettings,
} from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { Select } from '../components/Select'
import './AdminHome.css'
import './AdminUsers.css'

function formatBytes(n: number) {
  if (n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  return `${(n / 1024 ** i).toFixed(1)} ${units[i]}`
}

const INTERVAL_OPTIONS = [
  { value: '6', label: 'Every 6 hours' },
  { value: '12', label: 'Every 12 hours' },
  { value: '24', label: 'Daily' },
  { value: '168', label: 'Weekly' },
]

export function AdminBackups() {
  const [error, setError] = useState<string | null>(null)

  // Manual backup/restore -- unchanged from when this lived on the Dashboard.
  const [restoreFile, setRestoreFile] = useState<File | null>(null)
  const [restoreConfirmOpen, setRestoreConfirmOpen] = useState(false)
  const [restoring, setRestoring] = useState(false)
  const [restoreMessage, setRestoreMessage] = useState<{ ok: boolean; text: string } | null>(null)
  const restoreFileInputRef = useRef<HTMLInputElement>(null)

  // Automated backup schedule + the list of what it's produced so far.
  const [settings, setSettings] = useState<BackupSettings | null>(null)
  const [savingSettings, setSavingSettings] = useState(false)
  const [autoBackups, setAutoBackups] = useState<AutoBackup[]>([])
  const [restoreAutoTarget, setRestoreAutoTarget] = useState<string | null>(null)
  const [autoActionBusy, setAutoActionBusy] = useState<string | null>(null)

  function loadAutoBackups() {
    listAutoBackups()
      .then(setAutoBackups)
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }

  useEffect(() => {
    fetchBackupSettings()
      .then(setSettings)
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
    loadAutoBackups()
  }, [])

  async function handleRestore() {
    if (!restoreFile) return
    setRestoring(true)
    setRestoreMessage(null)
    try {
      await restoreBackup(restoreFile)
      setRestoreMessage({ ok: true, text: 'Restore completed. The server is restarting; this page will stop responding for a few seconds.' })
    } catch (err) {
      setRestoreMessage({ ok: false, text: err instanceof ApiError ? err.message : 'Restore failed' })
    } finally {
      setRestoreConfirmOpen(false)
      setRestoring(false)
      setRestoreFile(null)
      if (restoreFileInputRef.current) restoreFileInputRef.current.value = ''
    }
  }

  async function handleToggleEnabled(enabled: boolean) {
    if (!settings) return
    setSavingSettings(true)
    try {
      setSettings(await updateBackupSettings({ ...settings, enabled }))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to save backup settings')
    } finally {
      setSavingSettings(false)
    }
  }

  async function handleIntervalChange(value: string) {
    if (!settings) return
    setSavingSettings(true)
    try {
      setSettings(await updateBackupSettings({ ...settings, intervalHours: Number(value) }))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to save backup settings')
    } finally {
      setSavingSettings(false)
    }
  }

  async function handleDeleteAuto(filename: string) {
    setAutoActionBusy(filename)
    try {
      await deleteAutoBackup(filename)
      setAutoBackups((list) => list.filter((b) => b.filename !== filename))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete backup')
    } finally {
      setAutoActionBusy(null)
    }
  }

  async function handleRestoreAuto(filename: string) {
    setAutoActionBusy(filename)
    try {
      await restoreAutoBackup(filename)
      setRestoreMessage({ ok: true, text: 'Restore completed. The server is restarting; this page will stop responding for a few seconds.' })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Restore failed')
    } finally {
      setRestoreAutoTarget(null)
      setAutoActionBusy(null)
    }
  }

  return (
    <section className="vorn-admin-page">
      <div className="vorn-admin-page-header">
        <h1>Backups</h1>
        <p className="vorn-admin-page-subtitle">
          Everything Vorn stores — users, libraries, media metadata, and every admin credential (TMDb, debrid
          accounts, Usenet servers, ...) — lives in one Postgres database, so a backup here is a complete snapshot,
          not just the media library.
        </p>
      </div>
      {error && <p className="vorn-form-error">{error}</p>}

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Automated backups</h2>
        </div>
        <p className="vorn-panel-subtitle">
          Runs on a schedule and keeps the 5 most recent, deleting older ones automatically. Stored on this server's
          disk, not downloaded anywhere — use the manual backup below if you want a copy off this machine.
        </p>
        {settings && (
          <div className="vorn-inline-form">
            <label>
              <input type="checkbox" checked={settings.enabled} disabled={savingSettings} onChange={(e) => handleToggleEnabled(e.target.checked)} />{' '}
              Enable automated backups
            </label>
            <Select
              value={String(settings.intervalHours)}
              onChange={handleIntervalChange}
              options={INTERVAL_OPTIONS}
              disabled={!settings.enabled || savingSettings}
            />
          </div>
        )}

        {autoBackups.length === 0 ? (
          <p>No automated backups yet.</p>
        ) : (
          <div className="vorn-table-wrap">
            <table className="vorn-table">
              <thead>
                <tr>
                  <th>Created</th>
                  <th>Size</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {autoBackups.map((b) => (
                  <tr key={b.filename}>
                    <td>{new Date(b.createdAt).toLocaleString()}</td>
                    <td>{formatBytes(b.sizeBytes)}</td>
                    <td>
                      <div className="vorn-button-group">
                        <a href={autoBackupDownloadUrl(b.filename)} className="vorn-link-button-plain" download>
                          Download
                        </a>
                        <button
                          type="button"
                          onClick={() => setRestoreAutoTarget(b.filename)}
                          disabled={autoActionBusy === b.filename}
                        >
                          Restore
                        </button>
                        <button
                          type="button"
                          className="vorn-btn-danger"
                          onClick={() => handleDeleteAuto(b.filename)}
                          disabled={autoActionBusy === b.filename}
                        >
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Manual backup</h2>
        </div>
        <p className="vorn-panel-subtitle">
          Downloads straight to your own machine. Treat the file as sensitive: it contains the credentials above in
          plain text.
        </p>
        <div className="vorn-button-group">
          <a href={backupDownloadUrl()} className="vorn-link-button" download>
            Download backup
          </a>
        </div>

        <p className="vorn-panel-subtitle" style={{ marginTop: '1.25rem' }}>
          Restoring <strong>replaces the entire database</strong> with the contents of the uploaded file, discarding
          everything currently in it, then restarts the server. There is no undo — take a fresh backup first if
          you're not certain.
        </p>
        <div className="vorn-button-group">
          <input
            ref={restoreFileInputRef}
            type="file"
            accept=".sql"
            onChange={(e) => setRestoreFile(e.target.files?.[0] ?? null)}
          />
          <button
            type="button"
            className="vorn-btn-danger"
            onClick={() => setRestoreConfirmOpen(true)}
            disabled={!restoreFile || restoring}
          >
            {restoring ? 'Restoring…' : 'Restore from backup'}
          </button>
        </div>
        {restoreMessage && <p className={restoreMessage.ok ? 'vorn-test-result-ok' : 'vorn-form-error'}>{restoreMessage.text}</p>}
      </div>

      {restoreConfirmOpen && (
        <ConfirmDialog
          title="Restore from backup?"
          message={`This replaces the entire database with "${restoreFile?.name}", permanently discarding everything currently in it (users, libraries, all settings), then restarts the server. This cannot be undone.`}
          confirmLabel="Restore"
          danger
          busy={restoring}
          onConfirm={handleRestore}
          onCancel={() => setRestoreConfirmOpen(false)}
        />
      )}

      {restoreAutoTarget && (
        <ConfirmDialog
          title="Restore from this backup?"
          message={`This replaces the entire database with "${restoreAutoTarget}", permanently discarding everything currently in it (users, libraries, all settings), then restarts the server. This cannot be undone.`}
          confirmLabel="Restore"
          danger
          busy={autoActionBusy === restoreAutoTarget}
          onConfirm={() => handleRestoreAuto(restoreAutoTarget)}
          onCancel={() => setRestoreAutoTarget(null)}
        />
      )}
    </section>
  )
}
