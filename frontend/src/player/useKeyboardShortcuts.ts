import { useEffect } from 'react'

export interface KeyboardShortcutHandlers {
  onTogglePlay: () => void
  onSeek: (deltaSeconds: number) => void
  onSeekToFraction: (fraction: number) => void
  onVolumeChange: (delta: number) => void
  onToggleMute: () => void
  onToggleFullscreen: () => void
  onSpeedChange: (delta: number) => void
}

const SEEK_STEP_SECONDS = 10
const VOLUME_STEP = 0.1
const SPEED_STEP = 0.25

function isTypingTarget(el: EventTarget | null): boolean {
  if (!(el instanceof HTMLElement)) return false
  return el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' || el.isContentEditable
}

// Attaches document-level playback shortcuts while the player is mounted --
// scoped to the watch page's lifetime (via the enabled flag/effect cleanup)
// rather than always-on, so navigating away unregisters them. Ignored while
// a form control has focus, though the watch page doesn't have one today.
export function useKeyboardShortcuts(handlers: KeyboardShortcutHandlers, enabled: boolean) {
  useEffect(() => {
    if (!enabled) return

    function handleKeyDown(e: KeyboardEvent) {
      if (isTypingTarget(e.target)) return

      switch (e.key) {
        case ' ':
        case 'k':
          e.preventDefault()
          handlers.onTogglePlay()
          break
        case 'ArrowLeft':
          e.preventDefault()
          handlers.onSeek(-SEEK_STEP_SECONDS)
          break
        case 'ArrowRight':
          e.preventDefault()
          handlers.onSeek(SEEK_STEP_SECONDS)
          break
        case 'ArrowUp':
          e.preventDefault()
          handlers.onVolumeChange(VOLUME_STEP)
          break
        case 'ArrowDown':
          e.preventDefault()
          handlers.onVolumeChange(-VOLUME_STEP)
          break
        case 'm':
          handlers.onToggleMute()
          break
        case 'f':
          handlers.onToggleFullscreen()
          break
        case ',':
          handlers.onSpeedChange(-SPEED_STEP)
          break
        case '.':
          handlers.onSpeedChange(SPEED_STEP)
          break
        default:
          if (e.key >= '0' && e.key <= '9') {
            handlers.onSeekToFraction(Number(e.key) / 10)
          }
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [enabled, handlers])
}
