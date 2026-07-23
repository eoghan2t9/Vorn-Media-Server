import { forwardRef, useImperativeHandle, useRef, useState, type DragEvent } from 'react'
import { FileIcon, UploadIcon, XIcon } from './icons'
import './FileDropzone.css'

export interface FileDropzoneHandle {
  reset: () => void
}

/**
 * A styled drag-and-drop file picker. Selecting a local file always goes
 * through the browser's native file dialog underneath (there is no way
 * around that for reading the user's own filesystem) -- this only replaces
 * the ugly unstyled native <input type="file"> control with a themed
 * dropzone, it doesn't and can't remove the OS picker itself.
 */
export const FileDropzone = forwardRef<
  FileDropzoneHandle,
  {
    accept?: string
    hint: string
    onFile: (file: File) => void
    disabled?: boolean
  }
>(function FileDropzone({ accept, hint, onFile, disabled }, ref) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [fileName, setFileName] = useState<string | null>(null)
  const [dragOver, setDragOver] = useState(false)

  useImperativeHandle(ref, () => ({
    reset() {
      setFileName(null)
      if (inputRef.current) inputRef.current.value = ''
    },
  }))

  function handleFiles(files: FileList | null) {
    const file = files?.[0]
    if (!file) return
    setFileName(file.name)
    onFile(file)
  }

  function handleDrop(e: DragEvent<HTMLDivElement>) {
    e.preventDefault()
    setDragOver(false)
    if (disabled) return
    handleFiles(e.dataTransfer.files)
  }

  return (
    <div
      className={`vorn-dropzone${dragOver ? ' vorn-dropzone-active' : ''}${disabled ? ' vorn-dropzone-disabled' : ''}`}
      onClick={() => !disabled && inputRef.current?.click()}
      onDragOver={(e) => {
        e.preventDefault()
        if (!disabled) setDragOver(true)
      }}
      onDragLeave={() => setDragOver(false)}
      onDrop={handleDrop}
      role="button"
      tabIndex={disabled ? -1 : 0}
      onKeyDown={(e) => {
        if ((e.key === 'Enter' || e.key === ' ') && !disabled) {
          e.preventDefault()
          inputRef.current?.click()
        }
      }}
    >
      <input
        ref={inputRef}
        type="file"
        accept={accept}
        disabled={disabled}
        className="vorn-dropzone-input"
        onChange={(e) => handleFiles(e.target.files)}
      />
      {fileName ? (
        <div className="vorn-dropzone-file">
          <FileIcon className="vorn-dropzone-icon" />
          <span className="vorn-dropzone-filename">{fileName}</span>
          {!disabled && (
            <button
              type="button"
              className="vorn-dropzone-clear"
              onClick={(e) => {
                e.stopPropagation()
                setFileName(null)
                if (inputRef.current) inputRef.current.value = ''
              }}
              aria-label="Remove selected file"
            >
              <XIcon />
            </button>
          )}
        </div>
      ) : (
        <div className="vorn-dropzone-prompt">
          <UploadIcon className="vorn-dropzone-icon" />
          <span>
            <strong>Click to browse</strong> or drag a file here
          </span>
          <span className="vorn-dropzone-hint">{hint}</span>
        </div>
      )}
    </div>
  )
})
