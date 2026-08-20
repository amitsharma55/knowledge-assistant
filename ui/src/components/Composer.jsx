import { useRef, useState } from 'react';

export default function Composer({ onSend, onUpload, busy, uploads, onRemoveUpload }) {
  const [text, setText] = useState('');
  const [persist, setPersist] = useState(false);
  const fileRef = useRef();

  function submit() {
    const t = text.trim();
    if (!t || busy) return;
    setText('');
    onSend(t);
  }

  return (
    <div className="border-t border-slate-200 bg-white p-3">
      {uploads.length > 0 && (
        <div className="flex flex-wrap gap-2 mb-2">
          {uploads.map(u => (
            <span key={u.uploadId} className="inline-flex items-center gap-1 px-2 py-1 text-xs rounded-full bg-blue-50 text-blue-800 border border-blue-200">
              {u.filename} · {u.chunks} chunks{u.persisted ? ' · saved' : ''}
              <button onClick={() => onRemoveUpload(u.uploadId)} className="text-blue-500 hover:text-blue-700">×</button>
            </span>
          ))}
        </div>
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
            title="Attach a document"
            onClick={() => fileRef.current?.click()}
            disabled={busy}
            className="w-9 h-9 rounded-md border border-slate-300 text-slate-600 hover:bg-slate-50 disabled:opacity-50">
            📎
          </button>
          <label className="flex items-center gap-1 text-[10px] text-slate-500 whitespace-nowrap">
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
          disabled={busy}
          placeholder="Ask about an integration, its fields, its jobs…"
          className="flex-1 resize-none rounded-md border border-slate-300 px-3 py-2 focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
        <button
          onClick={submit}
          disabled={busy || !text.trim()}
          className="h-9 px-4 rounded-md bg-blue-600 text-white text-sm hover:bg-blue-700 disabled:bg-slate-300 disabled:cursor-not-allowed">
          Send
        </button>
      </div>
    </div>
  );
}
