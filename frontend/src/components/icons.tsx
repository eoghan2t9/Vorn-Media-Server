import type { SVGProps } from 'react'

type IconProps = SVGProps<SVGSVGElement>

const base = {
  width: 18,
  height: 18,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.8,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
}

export function DashboardIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <rect x="3" y="3" width="7.5" height="7.5" rx="1.5" />
      <rect x="13.5" y="3" width="7.5" height="7.5" rx="1.5" />
      <rect x="3" y="13.5" width="7.5" height="7.5" rx="1.5" />
      <rect x="13.5" y="13.5" width="7.5" height="7.5" rx="1.5" />
    </svg>
  )
}

export function UsersIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <circle cx="9" cy="8" r="3.25" />
      <path d="M2.75 20c0-3.5 2.8-6 6.25-6s6.25 2.5 6.25 6" />
      <circle cx="17" cy="8.5" r="2.5" />
      <path d="M15.5 14.15c2.7.5 4.75 2.65 4.75 5.85" />
    </svg>
  )
}

export function LibraryIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M3 9h18" />
      <path d="M10 13.2l5 2.9v-5.8z" />
    </svg>
  )
}

export function EyeIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7-10-7-10-7z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  )
}

export function MagnetIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M6 4v7a6 6 0 0 0 12 0V4" />
      <path d="M4 4h4M16 4h4M4 4v5M20 4v5" />
      <path d="M5.5 12.5h3M15.5 12.5h3" />
    </svg>
  )
}

export function CloudDownloadIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M7.5 18a4.5 4.5 0 0 1-.9-8.9 5.5 5.5 0 0 1 10.7-1.7A4.25 4.25 0 0 1 17 18Z" />
      <path d="M12 10.5v6M9.25 14.25 12 17l2.75-2.75" />
    </svg>
  )
}

export function CloudIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M7.5 18a4.5 4.5 0 0 1-.9-8.9 5.5 5.5 0 0 1 10.7-1.7A4.25 4.25 0 0 1 17 18Z" />
    </svg>
  )
}

export function PlugIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M9 3v5M15 3v5" />
      <rect x="6.5" y="8" width="11" height="6.5" rx="2" />
      <path d="M12 14.5V18" />
      <path d="M8.5 21h7" />
    </svg>
  )
}

export function GlobeIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <circle cx="12" cy="12" r="9" />
      <path d="M3 12h18" />
      <path d="M12 3c2.5 2.5 3.8 5.7 3.8 9s-1.3 6.5-3.8 9c-2.5-2.5-3.8-5.7-3.8-9s1.3-6.5 3.8-9z" />
    </svg>
  )
}

export function TerminalIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M7 9l3.5 3L7 15" />
      <path d="M13 15h4" />
    </svg>
  )
}

export function ChevronRightIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M9 5l7 7-7 7" />
    </svg>
  )
}

export function MenuIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M4 6h16M4 12h16M4 18h16" />
    </svg>
  )
}
