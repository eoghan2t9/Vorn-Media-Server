import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  ApiError,
  decideContentRequest,
  listAdminContentRequests,
  type ContentRequest,
  type ContentRequestStatus,
} from '../api/client'
import { Select } from '../components/Select'
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

  function load() {
    listAdminContentRequests((statusFilter || undefined) as ContentRequestStatus | undefined)
      .then(setRequests)
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }

  useEffect(load, [statusFilter])

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

  return (
    <section className="vorn-admin-page">
      <div className="vorn-admin-page-header">
        <h1>Requests</h1>
        <p className="vorn-admin-page-subtitle">Review titles users have asked for.</p>
      </div>
      {error && <p className="vorn-form-error">{error}</p>}

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
                {requests.map((r) => (
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
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </section>
  )
}
