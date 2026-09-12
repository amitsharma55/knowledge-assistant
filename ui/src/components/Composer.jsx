import { useRef, useState } from 'react';
import { PaperclipIcon, SendIcon, StopIcon, CloseIcon } from '../icons.jsx';

export default function Composer({ onSend, onUpload, onStop, streaming, uploading, disabled, uploads, onRemoveUpload }) {
  const [text, setText] = useState('');
  const [persist, setPersist] = useState(false);
  const fileRef = useRef();

  function submit() {
    const t = text.trim();
    if (!t || streaming || disabled) return;
    setText('');
    onSend(t);
  }

  return (
    <div className="border-t border-rule bg-surface p-3">
      {uploads.length > 0 && (
        <ul className="flex flex-wrap gap-2 mb-2">
          {uploads.map(u => (
            <li key={u.uploadId} className="inline-flex items-center gap-1 px-2 py-1 text-xs rounded-full bg-info-bg text-info border border-info-bg">
              {u.filename} · {u.chunks} chunks{u.persisted ? ' · saved' : ''}
              <button
                onClick={() => onRemoveUpload(u.uploadId)}
                aria-label={`Remove ${u.filename}`}
                className="text-info hover:text-info p-0.5">
                <CloseIcon className="w-3 h-3" />
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="flex items-end gap-2">
        <div className="flex flex-col gap-1 items-center">
          <input
            ref={fileRef}
            type="file"
            accept=".pdf,.md,.txt,.markdown"
            className="hidden"
            onChange={e => {
              const f = e.target.files?.[0];
              e.target.value = '';
              if (f) onUpload(f, persist);
            }}
          />
          <button
            aria-label="Attach a document"
            onClick={() => fileRef.current?.click()}
            disabled={uploading || disabled}
            className="w-9 h-9 flex items-center justify-center rounded-md border border-rule-2 text-ink-2 hover:bg-surface-2 disabled:opacity-50 disabled:cursor-not-allowed">
            {uploading
              ? <span className="w-3.5 h-3.5 rounded-full border-2 border-rule-2 border-t-ink-3 animate-spin" />
              : <PaperclipIcon />}
          </button>
          <label className="flex items-center gap-1 text-[11px] text-ink-2 whitespace-nowrap">
            <input
              type="checkbox"
              checked={persist}
              onChange={e => setPersist(e.target.checked)}
              className="w-3 h-3"
            />
            Save to KB
          </label>
        </div>
        <textarea
          value={text}
          onChange={e => setText(e.target.value)}
          onKeyDown={e => {
            if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); submit(); }
          }}
          rows={2}
          /* Deliberately still editable while streaming, so the next question
             can be drafted while the current answer arrives. Only a missing
             team disables it. */
          disabled={disabled}
          aria-label="Ask a question"
          placeholder="Ask about an integration, its fields, its jobs…"
          className="flex-1 resize-none rounded-md border border-rule-2 px-3 py-2 focus:outline-none focus:ring-2 focus:ring-accent-edge disabled:bg-paper"
        />
        {streaming ? (
          <button
            onClick={onStop}
            className="h-9 px-4 inline-flex items-center gap-1.5 rounded-md border border-rule-2 text-ink-2 text-sm hover:bg-surface-2">
            <StopIcon className="w-3 h-3" />
            Stop
          </button>
        ) : (
          <button
            onClick={submit}
            disabled={disabled || !text.trim()}
            className="h-9 px-4 inline-flex items-center gap-1.5 rounded-md bg-ink text-white text-sm hover:bg-ink/85 disabled:bg-rule-2 disabled:cursor-not-allowed">
            <SendIcon className="w-3.5 h-3.5" />
            Send
          </button>
        )}
      </div>
    </div>
  );
}
