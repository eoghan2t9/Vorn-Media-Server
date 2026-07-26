import './Pagination.css'

export function Pagination({
  page,
  totalPages,
  onChange,
}: {
  page: number
  totalPages: number
  onChange: (page: number) => void
}) {
  if (totalPages <= 1) return null

  return (
    <div className="vorn-pagination">
      <button type="button" onClick={() => onChange(page - 1)} disabled={page <= 1}>
        Prev
      </button>
      <span>
        Page {page} of {totalPages}
      </span>
      <button type="button" onClick={() => onChange(page + 1)} disabled={page >= totalPages}>
        Next
      </button>
    </div>
  )
}
