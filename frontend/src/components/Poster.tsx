import type { ReactNode } from 'react'
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
  return (
    <div className={`vorn-poster${className ? ` ${className}` : ''}`}>
      {posterUrl ? (
        <img src={posterUrl} alt="" loading="lazy" className="vorn-poster-img" />
      ) : (
        <div className="vorn-poster-fallback" aria-hidden>
          <span>{title}</span>
        </div>
      )}
      {children}
    </div>
  )
}
