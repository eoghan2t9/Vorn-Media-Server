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

export function InboxIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M3 12h4.5l1.5 3h6l1.5-3H21" />
      <path d="M5.5 5h13l2.5 7v6.5a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 18.5V12z" />
    </svg>
  )
}

export function ArchiveIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <rect x="3" y="3.5" width="18" height="4.5" rx="1.2" />
      <path d="M4.5 8v10a1.5 1.5 0 0 0 1.5 1.5h12a1.5 1.5 0 0 0 1.5-1.5V8" />
      <path d="M10 13h4" />
    </svg>
  )
}

export function BellIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M6 9.5a6 6 0 0 1 12 0c0 4.5 1.5 6 1.5 6h-15s1.5-1.5 1.5-6z" />
      <path d="M10 19.5a2 2 0 0 0 4 0" />
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

export function InfoIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v6" />
      <circle cx="12" cy="7.5" r="0.9" fill="currentColor" stroke="none" />
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

export function ChevronDownIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M6 9l6 6 6-6" />
    </svg>
  )
}

export function CheckIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M5 12.5l4.5 4.5L19 7" />
    </svg>
  )
}

export function FolderIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M3.5 6.5a1.5 1.5 0 0 1 1.5-1.5h4l2 2.5h8a1.5 1.5 0 0 1 1.5 1.5v8.5a1.5 1.5 0 0 1-1.5 1.5h-14a1.5 1.5 0 0 1-1.5-1.5z" />
    </svg>
  )
}

export function UploadIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M12 16V4M8 8l4-4 4 4" />
      <path d="M4 15v3a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-3" />
    </svg>
  )
}

export function FileIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M7 3.5h7l4 4v13a1 1 0 0 1-1 1H7a1 1 0 0 1-1-1v-16a1 1 0 0 1 1-1z" />
      <path d="M14 3.5V8h4.5" />
    </svg>
  )
}

export function XIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M6 6l12 12M18 6L6 18" />
    </svg>
  )
}

export function CpuIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <rect x="7" y="7" width="10" height="10" rx="1.5" />
      <rect x="10" y="2.5" width="4" height="3" />
      <rect x="10" y="17.5" width="4" height="3" />
      <rect x="2.5" y="10" width="3" height="4" />
      <rect x="17.5" y="10" width="3" height="4" />
    </svg>
  )
}

export function NetworkIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M8 16V6M8 6L4.5 9.5M8 6l3.5 3.5" />
      <path d="M16 8v10M16 18l3.5-3.5M16 18l-3.5-3.5" />
    </svg>
  )
}

export function MemoryIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <rect x="3" y="6" width="18" height="12" rx="1.5" />
      <path d="M7 6V3.5M11 6V3.5M13 6V3.5M17 6V3.5" />
      <path d="M7 20.5V18M11 20.5V18M13 20.5V18M17 20.5V18" />
      <path d="M6.5 10h3v4h-3z" />
    </svg>
  )
}

export function HardDriveIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <rect x="2.5" y="4" width="19" height="16" rx="2" />
      <path d="M2.5 14h19" />
      <path d="M6 17.2h.01M10 17.2h.01" />
    </svg>
  )
}

export function PlayIcon(props: IconProps) {
  return (
    <svg {...base} fill="currentColor" stroke="none" {...props}>
      <path d="M6.5 4.2c0-1 1.1-1.6 1.9-1.1l12.3 7.8c.8.5.8 1.7 0 2.2L8.4 20.9c-.8.5-1.9-.1-1.9-1.1z" />
    </svg>
  )
}

export function PauseIcon(props: IconProps) {
  return (
    <svg {...base} fill="currentColor" stroke="none" {...props}>
      <rect x="5.5" y="3.5" width="4.5" height="17" rx="1.2" />
      <rect x="14" y="3.5" width="4.5" height="17" rx="1.2" />
    </svg>
  )
}

export function SeekBackIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M4 12a8 8 0 1 0 2.7-6" />
      <path d="M3 4.5v4.5h4.5" />
      <text x="12" y="15.5" fontSize="6.5" fill="currentColor" stroke="none" textAnchor="middle" fontFamily="inherit">
        10
      </text>
    </svg>
  )
}

export function SeekForwardIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M20 12a8 8 0 1 1-2.7-6" />
      <path d="M21 4.5v4.5h-4.5" />
      <text x="12" y="15.5" fontSize="6.5" fill="currentColor" stroke="none" textAnchor="middle" fontFamily="inherit">
        10
      </text>
    </svg>
  )
}

export function VolumeIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M4 9.5h3.5L12 5.5v13L7.5 14.5H4z" />
      <path d="M15.5 9a4.5 4.5 0 0 1 0 6" />
      <path d="M18 6.5a8.5 8.5 0 0 1 0 11" />
    </svg>
  )
}

export function VolumeMuteIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M4 9.5h3.5L12 5.5v13L7.5 14.5H4z" />
      <path d="M15.5 9.5l4.5 5M20 9.5l-4.5 5" />
    </svg>
  )
}

export function GearIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <circle cx="12" cy="12" r="3" />
      <path d="M12 3.5v2.2M12 18.3v2.2M20.5 12h-2.2M5.7 12H3.5M17.8 6.2l-1.55 1.55M7.75 16.25 6.2 17.8M17.8 17.8l-1.55-1.55M7.75 7.75 6.2 6.2" />
    </svg>
  )
}

export function PipIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <rect x="2.5" y="4.5" width="19" height="14" rx="1.8" />
      <rect x="11.5" y="11" width="7.5" height="5.2" rx="1" fill="currentColor" stroke="none" />
    </svg>
  )
}

export function FullscreenIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M8 3.5H4.5V7M16 3.5h3.5V7M8 20.5H4.5V17M16 20.5h3.5V17" />
    </svg>
  )
}

export function FullscreenExitIcon(props: IconProps) {
  return (
    <svg {...base} {...props}>
      <path d="M4.5 8V4.5H8M19.5 8V4.5H16M4.5 16v3.5H8M19.5 16v3.5H16" />
    </svg>
  )
}
