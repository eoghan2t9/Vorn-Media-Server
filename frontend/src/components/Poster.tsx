import type { ReactNode } from 'react'
import { resolveMediaUrl } from '../api/client'
import './Poster.css'

export function Poster({
  title,
  posterUrl,
  className,
  children,
}: {
  title: string
  posterUrl?: string
  className?: string
  children?: ReactNode
}) {
  const resolvedUrl = resolveMediaUrl(posterUrl)
  return (
    <div className={`vorn-poster${className ? ` ${className}` : ''}`}>
      {resolvedUrl ? (
        <img src={resolvedUrl} alt="" loading="lazy" className="vorn-poster-img" />
      ) : (
        <div className="vorn-poster-fallback" aria-hidden>
          <span>{title}</span>
        </div>
      )}
      {children}
    </div>
  )
}
