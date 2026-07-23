import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { search, type MediaItem } from '../api/client'

export function HeaderSearch() {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<MediaItem[]>([])
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLDivElement>(null)
  const navigate = useNavigate()

  useEffect(() => {
    if (query.trim().length < 2) {
      setResults([])
      return
    }
    const handle = setTimeout(() => {
      search(query.trim())
        .then((items) => {
          setResults(items)
          setOpen(true)
        })
        .catch(() => setResults([]))
    }, 250)
    return () => clearTimeout(handle)
  }, [query])

  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  function goToItem(id: string) {
    setOpen(false)
    setQuery('')
    navigate(`/items/${id}`)
  }

  return (
    <div className="vorn-header-search" ref={containerRef}>
      <input
        type="search"
        placeholder="Search your library…"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onFocus={() => results.length > 0 && setOpen(true)}
      />
      {open && results.length > 0 && (
        <ul className="vorn-search-results">
          {results.map((r) => (
            <li key={r.id}>
              <button type="button" onClick={() => goToItem(r.id)}>
                {r.title}
                <span className="vorn-search-kind">{r.kind}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
