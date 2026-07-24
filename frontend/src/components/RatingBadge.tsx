export function RatingBadge({ rating }: { rating?: string | number }) {
  const n = typeof rating === 'string' ? parseFloat(rating) : rating
  if (!n) return null
  return <div className="vorn-rating-badge">★ {n.toFixed(1)}</div>
}
