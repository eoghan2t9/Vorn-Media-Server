import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  ApiError,
  decideContentRequest,
  deleteAdminContentRequest,
  fetchRequestSettings,
  listAdminContentRequests,
  updateRequestSettings,
  type ContentRequest,
  type ContentRequestStatus,
  type RequestSettings,
} from '../api/client'
import { Pagination } from '../components/Pagination'
import { Select } from '../components/Select'
import { usePagination } from '../components/usePagination'
import './AdminUsers.css'

const STATUS_FILTERS: { value: string; label: string }[] = [
  { value: 'pending', label: 'Pending' },
  { value: 'approved', label: 'Approved' },
  { value: 'declined', label: 'Declined' },
  { value: '', label: 'All' },
]

export function AdminRequests() {
  const navigate = useNavigate()
  const [statusFilter, setStatusFilter] = useState('pending')
  const [requests, setRequests] = useState<ContentRequest[]>([])
  const [error, setError] = useState<string | null>(null)
  const [decidingId, setDecidingId] = useState<string | null>(null)
  const [deletingId, setDeletingId] = useState<string | null>(null)
  const [settings, setSettings] = useState<RequestSettings | null>(null)
  const [savingSettings, setSavingSettings] = useState(false)
  const requestsPage = usePagination(requests)

  function load() {
    listAdminContentRequests((statusFilter || undefined) as ContentRequestStatus | undefined)
      .then(setRequests)
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }

  useEffect(load, [statusFilter])

  useEffect(() => {
    fetchRequestSettings()
      .then(setSettings)
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [])

  async function handleToggleAutoApprove(autoApprove: boolean) {
    setSavingSettings(true)
    try {
      setSettings(await updateRequestSettings({ autoApprove }))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to save request settings')
    } finally {
      setSavingSettings(false)
    }
  }
  // Changing the status filter swaps in an entirely different set of
  // requests -- staying on, say, page 3 of "Pending" after switching to
  // "Declined" would likely just show an empty/truncated page instead of
  // that filter's actual first results.
  useEffect(() => requestsPage.setPage(1), [statusFilter])

  async function handleDecide(id: string, status: 'approved' | 'declined') {
    setError(null)
    setDecidingId(id)
    try {
      await decideContentRequest(id, status)
      load()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to update request')
    } finally {
      setDecidingId(null)
    }
  }

  // Unlike a user withdrawing their own pending request, an admin can
  // remove any request regardless of status -- e.g. one that was approved
  // but never usefully fulfilled (no default request target configured at
  // the time), or just tidying up an old declined one.
  async function handleDelete(id: string) {
    setError(null)
    setDeletingId(id)
    try {
      await deleteAdminContentRequest(id)
      setRequests((list) => list.filter((r) => r.id !== id))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to delete request')
    } finally {
      setDeletingId(null)
    }
  }

  return (
    <section className="vorn-admin-page">
      <div className="vorn-admin-page-header">
        <h1>Requests</h1>
        <p className="vorn-admin-page-subtitle">Review titles users have asked for.</p>
      </div>
      {error && <p className="vorn-form-error">{error}</p>}

      {settings && (
        <div className="vorn-panel">
          <div className="vorn-inline-form">
            <label>
              <input
                type="checkbox"
                checked={settings.autoApprove}
                disabled={savingSettings}
                onChange={(e) => handleToggleAutoApprove(e.target.checked)}
              />{' '}
              Auto-approve requests
            </label>
            <p className="vorn-admin-page-subtitle">
              When on, new requests skip this queue and start fulfilling immediately instead of waiting for approval.
            </p>
          </div>
        </div>
      )}

      <div className="vorn-panel">
        <div className="vorn-panel-header">
          <h2>Queue</h2>
          <Select value={statusFilter} onChange={setStatusFilter} options={STATUS_FILTERS} />
        </div>
        {requests.length === 0 ? (
          <p>Nothing here.</p>
        ) : (
          <div className="vorn-table-wrap">
            <table className="vorn-table">
              <thead>
                <tr>
                  <th>Title</th>
                  <th>Type</th>
                  <th>Requested by</th>
                  <th>Status</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {requestsPage.pageItems.map((r) => (
                  <tr key={r.id}>
                    <td>{r.title}</td>
                    <td>{r.mediaType}</td>
                    <td>{r.requester}</td>
                    <td>
                      <span className={`vorn-status-badge vorn-status-badge-${r.status}`}>{r.status}</span>
                    </td>
                    <td>
                      <div className="vorn-button-group">
                        {r.status === 'pending' && (
                          <>
                            <button
                              type="button"
                              onClick={() => handleDecide(r.id, 'approved')}
                              disabled={decidingId === r.id}
                            >
                              Approve
                            </button>
                            <button
                              type="button"
                              className="vorn-btn-danger"
                              onClick={() => handleDecide(r.id, 'declined')}
                              disabled={decidingId === r.id}
                            >
                              Decline
                            </button>
                          </>
                        )}
                        <button type="button" onClick={() => navigate(`/admin/torrents?q=${encodeURIComponent(r.title)}`)}>
                          Search torrents
                        </button>
                        <button
                          type="button"
                          className="vorn-btn-danger"
                          onClick={() => handleDelete(r.id)}
                          disabled={deletingId === r.id}
                        >
                          {deletingId === r.id ? 'Deleting…' : 'Delete'}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <Pagination page={requestsPage.page} totalPages={requestsPage.totalPages} onChange={requestsPage.setPage} />
      </div>
    </section>
  )
}
