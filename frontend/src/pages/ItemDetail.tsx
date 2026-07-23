import { useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ApiError, getItem, updateItemMetadata, type MediaItemDetail } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import './ItemDetail.css'

function EditMetadataForm({ item, onSaved }: { item: MediaItemDetail; onSaved: (updated: MediaItemDetail) => void }) {
  const [title, setTitle] = useState(item.title)
  const [overview, setOverview] = useState(item.overview ?? '')
  const [releaseDate, setReleaseDate] = useState(item.releaseDate ?? '')
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSaving(true)
    try {
      const updated = await updateItemMetadata(item.id, { title, overview, releaseDate: releaseDate || undefined })
      onSaved({ ...item, ...updated })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Failed to save metadata')
    } finally {
      setSaving(false)
    }
  }

  return (
    <form className="vorn-form vorn-metadata-edit" onSubmit={handleSubmit}>
      <h2>Correct metadata</h2>
      <p className="vorn-form-subtitle">
        Use this if the item was misidentified. Saving locks it so a future metadata sync won't overwrite the fix.
      </p>
      <label>
        Title
        <input value={title} onChange={(e) => setTitle(e.target.value)} required />
      </label>
      <label>
        Overview
        <textarea value={overview} onChange={(e) => setOverview(e.target.value)} rows={4} />
      </label>
      <label>
        Release date
        <input type="date" value={releaseDate} onChange={(e) => setReleaseDate(e.target.value)} />
      </label>
      {error && <p className="vorn-form-error">{error}</p>}
      <button type="submit" disabled={saving}>
        {saving ? 'Saving…' : 'Save correction'}
      </button>
    </form>
  )
}

export function ItemDetail() {
  const { id } = useParams<{ id: string }>()
  const { user } = useAuth()
  const [item, setItem] = useState<MediaItemDetail | null>(null)
  const [editing, setEditing] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!id) return
    getItem(id)
      .then(setItem)
      .catch((err) => setError(err instanceof ApiError ? err.message : String(err)))
  }, [id])

  if (error) return <p className="vorn-form-error">{error}</p>
  if (!item) return <p>Loading…</p>

  return (
    <section>
      <h1>{item.title}</h1>
      {item.releaseDate && <p>{item.releaseDate.slice(0, 4)}</p>}
      {item.overview && <p>{item.overview}</p>}

      {item.children && item.children.length > 0 && (
        <ul className="vorn-children-list">
          {item.children.map((c) => (
            <li key={c.id}>
              <Link to={`/items/${c.id}`}>{c.title}</Link>
            </li>
          ))}
        </ul>
      )}

      {user?.isAdmin && (item.kind === 'movie' || item.kind === 'series') && (
        <>
          {editing ? (
            <EditMetadataForm
              item={item}
              onSaved={(updated) => {
                setItem(updated)
                setEditing(false)
              }}
            />
          ) : (
            <button type="button" onClick={() => setEditing(true)}>
              Edit metadata
            </button>
          )}
        </>
      )}
    </section>
  )
}
