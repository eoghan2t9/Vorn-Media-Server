import { useEffect, useRef, useState, type RefObject } from 'react'
import { CheckIcon, ChevronDownIcon } from './icons'
import './Select.css'

export interface SelectOption {
  value: string
  label: string
}

function useCloseOnOutsideOrEscape(open: boolean, ref: RefObject<HTMLElement | null>, onClose: () => void) {
  useEffect(() => {
    if (!open) return
    function handlePointerDown(e: PointerEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
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
  }, [open, ref, onClose])
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
  useCloseOnOutsideOrEscape(open, rootRef, () => setOpen(false))

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
      {open && (
        <ul className="vorn-select-panel" role="listbox">
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
        </ul>
      )}
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
  useCloseOnOutsideOrEscape(open, rootRef, () => setOpen(false))

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
      {open && (
        <ul className="vorn-select-panel" role="listbox" aria-multiselectable="true">
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
        </ul>
      )}
    </div>
  )
}
