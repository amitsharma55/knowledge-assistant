import { useCallback, useEffect, useState } from 'react';
import { listPending, approvePending, rejectPending } from '../api';

// AdminPanel is the human review queue: documents uploaded with "Save to KB"
// wait here until an admin approves them into the index or rejects them. The
// list is team-scoped by the API, so it reflects the currently selected team.
export default function AdminPanel({ team }) {
  const [items, setItems] = useState([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);

  const refresh = useCallback(() => {
    if (!team) return;
    listPending(team).then(setItems).catch(() => setItems([]));
  }, [team]);

  useEffect(() => { refresh(); }, [refresh]);

  async function act(id, fn) {
    setBusy(true);
    setError(null);
    try {
      await fn(id, team);
      refresh();
    } catch (e) {
      setError(e.message || 'Action failed.');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="p-3">
      <div className="flex items-center justify-between mb-2">
        <h2 className="text-sm font-semibold text-ink">Review queue</h2>
        <button onClick={refresh} className="text-xs text-ink-2 hover:text-ink">Refresh</button>
      </div>
      {error && <p className="text-xs text-danger mb-2">{error}</p>}
      {items.length === 0 ? (
        <p className="text-sm text-ink-3">No documents awaiting review.</p>
      ) : (
        <ul className="divide-y divide-rule">
          {items.map(it => (
            <li key={it.id} className="flex items-center justify-between gap-2 py-2">
              <span className="text-sm text-ink min-w-0 truncate">
                {it.filename}
                <span className="text-ink-3"> · {it.uploader}</span>
                {it.findings?.length ? (
                  <span className="text-amber-700"> · {it.findings.length} flag{it.findings.length === 1 ? '' : 's'}</span>
                ) : null}
              </span>
              <span className="flex gap-2 shrink-0">
                <button
                  disabled={busy}
                  onClick={() => act(it.id, approvePending)}
                  className="text-xs px-2 py-1 rounded border border-rule-2 text-emerald-700 hover:bg-surface-2 disabled:opacity-50">
                  Approve
                </button>
                <button
                  disabled={busy}
                  onClick={() => act(it.id, rejectPending)}
                  className="text-xs px-2 py-1 rounded border border-rule-2 text-danger hover:bg-surface-2 disabled:opacity-50">
                  Reject
                </button>
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
