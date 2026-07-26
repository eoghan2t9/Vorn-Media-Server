import { useMemo, useState } from 'react'

// Client-side only -- every paginated list in the admin UI (torrents, NZB
// downloads, debrid items, content requests) is still small enough for a
// self-hosted server that slicing an already-fetched array is simpler than
// adding LIMIT/OFFSET query params, a total-count response field, and
// page-state-surviving-a-2s-poll logic to each endpoint. Revisit with real
// server-side pagination if a deployment's history grows large enough for
// that fetch itself to become the bottleneck.
export function usePagination<T>(items: T[], pageSize = 15) {
  const [page, setPage] = useState(1)
  const totalPages = Math.max(1, Math.ceil(items.length / pageSize))
  // Clamp rather than reset to 1 on every items change -- items is a new
  // array reference on every poll (AdminTorrents/AdminNzb refetch every 2s),
  // and resetting there would kick the admin back to page 1 mid-review.
  // Only clamps down when the current page no longer exists (e.g. the last
  // item on the last page just got deleted).
  const currentPage = Math.min(page, totalPages)
  const pageItems = useMemo(() => {
    const start = (currentPage - 1) * pageSize
    return items.slice(start, start + pageSize)
  }, [items, currentPage, pageSize])

  return { page: currentPage, setPage, totalPages, pageItems }
}
