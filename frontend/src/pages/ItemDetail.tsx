import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { ApiError, getItem, type MediaItemDetail } from '../api/client'
import './ItemDetail.css'

export function ItemDetail() {
  const { id } = useParams<{ id: string }>()
  const [item, setItem] = useState<MediaItemDetail | null>(null)
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
    </section>
  )
}
