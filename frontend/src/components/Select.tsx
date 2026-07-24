import { useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode, type RefObject } from 'react'
import { createPortal } from 'react-dom'
import { CheckIcon, ChevronDownIcon } from './icons'
import './Select.css'

export interface SelectOption {
  value: string
  label: string
}

// Gap kept between the trigger and the panel, and the minimum breathing
// room kept between the panel and the viewport edge -- panels are
// fixed-positioned in viewport coordinates (see computePanelPosition), not
// relative to any scrolling ancestor, so these are real pixels, not rem.
const PANEL_GAP = 6
const VIEWPORT_MARGIN = 8
const PREFERRED_MAX_HEIGHT = 256 // matches the panel's old CSS max-height: 16rem
const MIN_USABLE_HEIGHT = 120 // below this, flipping direction wouldn't help much

interface PanelPosition extends CSSProperties {
  position: 'fixed'
  left: number
  width: number
  maxHeight: number
}

// Positions the dropdown panel in viewport (not document) coordinates so it
// can be portaled straight to <body> -- escaping both the "always opens
// downward, runs off the bottom of a short mobile viewport" problem and a
// second, unrelated bug where a trigger inside a horizontally-scrollable
// container (e.g. .vorn-table-wrap) would have its panel clipped by that
// container's own scroll box, since overflow-x != visible forces the CSS
// overflow spec to compute overflow-y as auto too -- there's no CSS-only
// way to scroll one axis without clipping the other.
function computePanelPosition(trigger: HTMLElement): PanelPosition {
  const rect = trigger.getBoundingClientRect()
  const viewportHeight = window.innerHeight
  const viewportWidth = window.innerWidth

  const spaceBelow = viewportHeight - rect.bottom - PANEL_GAP - VIEWPORT_MARGIN
  const spaceAbove = rect.top - PANEL_GAP - VIEWPORT_MARGIN
  const openUpward = spaceBelow < MIN_USABLE_HEIGHT && spaceAbove > spaceBelow

  const left = Math.min(Math.max(rect.left, VIEWPORT_MARGIN), Math.max(VIEWPORT_MARGIN, viewportWidth - rect.width - VIEWPORT_MARGIN))
  const maxHeight = Math.max(MIN_USABLE_HEIGHT, Math.min(PREFERRED_MAX_HEIGHT, openUpward ? spaceAbove : spaceBelow))

  return openUpward
    ? { position: 'fixed', left, width: rect.width, bottom: viewportHeight - rect.top + PANEL_GAP, maxHeight }
    : { position: 'fixed', left, width: rect.width, top: rect.bottom + PANEL_GAP, maxHeight }
}

// Renders children into a panel portaled to document.body, positioned
// against triggerRef by computePanelPosition, and kept in sync with
// scroll/resize while open (capture:true on scroll so it also catches
// scrolling inside an ancestor like .vorn-table-wrap, not just the window).
function DropdownPanel({
  open,
  triggerRef,
  panelRef,
  className,
  role,
  ariaMultiselectable,
  children,
}: {
  open: boolean
  triggerRef: RefObject<HTMLElement | null>
  panelRef: RefObject<HTMLUListElement | null>
  className: string
  role: string
  ariaMultiselectable?: boolean
  children: ReactNode
}) {
  const [style, setStyle] = useState<PanelPosition | null>(null)

  useLayoutEffect(() => {
    if (!open) {
      setStyle(null)
      return
    }
    function reposition() {
      if (triggerRef.current) setStyle(computePanelPosition(triggerRef.current))
    }
    reposition()
    window.addEventListener('resize', reposition)
    window.addEventListener('scroll', reposition, true)
    return () => {
      window.removeEventListener('resize', reposition)
      window.removeEventListener('scroll', reposition, true)
    }
  }, [open, triggerRef])

  if (!open || !style) return null
  return createPortal(
    <ul className={className} role={role} aria-multiselectable={ariaMultiselectable} style={style} ref={panelRef}>
      {children}
    </ul>,
    document.body,
  )
}

// Closes on a pointerdown/Escape that lands outside BOTH the trigger
// (rootRef) and the portaled panel (panelRef) -- the panel needs its own
// ref since it's no longer a DOM descendant of the trigger's wrapper once
// portaled, so a click on an option would otherwise register as "outside"
// and close the dropdown before the option's own onClick ever fires.
function useCloseOnOutsideOrEscape(
  open: boolean,
  rootRef: RefObject<HTMLElement | null>,
  panelRef: RefObject<HTMLElement | null>,
  onClose: () => void,
) {
  useLayoutEffect(() => {
    if (!open) return
    function handlePointerDown(e: PointerEvent) {
      const target = e.target as Node
      if (rootRef.current?.contains(target) || panelRef.current?.contains(target)) return
      onClose()
    }
    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('pointerdown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [open, rootRef, panelRef, onClose])
}

export function Select({
  options,
  value,
  onChange,
  placeholder = 'Select…',
  disabled,
  className,
}: {
  options: SelectOption[]
  value: string
  onChange: (value: string) => void
  placeholder?: string
  disabled?: boolean
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLUListElement>(null)
  useCloseOnOutsideOrEscape(open, rootRef, panelRef, () => setOpen(false))

  const selected = options.find((o) => o.value === value)

  return (
    <div className={`vorn-select${className ? ` ${className}` : ''}`} ref={rootRef}>
      <button
        type="button"
        className="vorn-select-trigger"
        onClick={() => setOpen((v) => !v)}
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span className={selected ? '' : 'vorn-select-placeholder'}>{selected ? selected.label : placeholder}</span>
        <ChevronDownIcon className={`vorn-select-chevron${open ? ' vorn-select-chevron-open' : ''}`} />
      </button>
      <DropdownPanel open={open} triggerRef={rootRef} panelRef={panelRef} className="vorn-select-panel" role="listbox">
        {options.map((o) => (
          <li key={o.value}>
            <button
              type="button"
              role="option"
              aria-selected={o.value === value}
              className={`vorn-select-option${o.value === value ? ' vorn-select-option-active' : ''}`}
              onClick={() => {
                onChange(o.value)
                setOpen(false)
              }}
            >
              <span className="vorn-select-option-label">{o.label}</span>
              {o.value === value && <CheckIcon className="vorn-select-check" />}
            </button>
          </li>
        ))}
      </DropdownPanel>
    </div>
  )
}

export function MultiSelect({
  options,
  value,
  onChange,
  placeholder = 'Select…',
  className,
}: {
  options: SelectOption[]
  value: string[]
  onChange: (value: string[]) => void
  placeholder?: string
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLUListElement>(null)
  useCloseOnOutsideOrEscape(open, rootRef, panelRef, () => setOpen(false))

  function toggle(v: string) {
    onChange(value.includes(v) ? value.filter((x) => x !== v) : [...value, v])
  }

  const summary =
    value.length === 0
      ? placeholder
      : value.length === 1
        ? (options.find((o) => o.value === value[0])?.label ?? '1 selected')
        : `${value.length} selected`

  return (
    <div className={`vorn-select${className ? ` ${className}` : ''}`} ref={rootRef}>
      <button
        type="button"
        className="vorn-select-trigger"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span className={value.length === 0 ? 'vorn-select-placeholder' : ''}>{summary}</span>
        <ChevronDownIcon className={`vorn-select-chevron${open ? ' vorn-select-chevron-open' : ''}`} />
      </button>
      <DropdownPanel
        open={open}
        triggerRef={rootRef}
        panelRef={panelRef}
        className="vorn-select-panel"
        role="listbox"
        ariaMultiselectable
      >
        {options.length === 0 && <li className="vorn-select-empty">No options</li>}
        {options.map((o) => {
          const checked = value.includes(o.value)
          return (
            <li key={o.value}>
              <button
                type="button"
                role="option"
                aria-selected={checked}
                className={`vorn-select-option${checked ? ' vorn-select-option-active' : ''}`}
                onClick={() => toggle(o.value)}
              >
                <span className={`vorn-select-checkbox${checked ? ' vorn-select-checkbox-checked' : ''}`}>
                  {checked && <CheckIcon className="vorn-select-check" />}
                </span>
                <span className="vorn-select-option-label">{o.label}</span>
              </button>
            </li>
          )
        })}
      </DropdownPanel>
    </div>
  )
}
