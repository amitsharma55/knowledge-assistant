import { useState } from 'react';
import { groupByDate } from '../dateGroups.js';
import { TrashIcon } from '../icons.jsx';

export default function Sidebar({ chats, currentChatId, onSelect, onDelete }) {
  const groups = groupByDate(chats);
  // Which row is asking to confirm. Deleting a conversation is not undoable and
  // the control used to be a bare × that fired on first click.
  const [confirming, setConfirming] = useState(null);

  return (
    <aside aria-label="Conversations" className="w-64 shrink-0 border-r border-rule bg-paper overflow-y-auto">
      {groups.length === 0 && (
        <p className="p-4 text-sm text-ink-3">No conversations yet. Ask a question to start one.</p>
      )}
      {groups.map(group => (
        <div key={group.label} className="pt-3">
          <h2 className="px-3 pb-1 text-[11px] font-medium text-ink-3">{group.label}</h2>
          <ul>
            {group.items.map(c => {
              const current = c.id === currentChatId;
              return (
                <li key={c.id} className="group relative flex items-center">
                  <button
                    onClick={() => onSelect(c.id)}
                    aria-current={current ? 'true' : undefined}
                    className={
                      'flex-1 min-w-0 text-left truncate px-3 py-2 text-sm ' +
                      (current ? 'bg-mute-bg text-ink' : 'text-ink-2 hover:bg-surface-2')
                    }>
                    {c.title}
                  </button>
                  {confirming === c.id ? (
                    <span className="flex items-center gap-1 pr-2">
                      <button
                        onClick={() => { setConfirming(null); onDelete(c.id); }}
                        className="text-xs px-1.5 py-1 rounded bg-red-600 text-white hover:bg-red-700">
                        Delete
                      </button>
                      <button
                        onClick={() => setConfirming(null)}
                        className="text-xs px-1.5 py-1 rounded text-ink-2 hover:bg-mute-bg">
                        Keep
                      </button>
                    </span>
                  ) : (
                    <button
                      onClick={() => setConfirming(c.id)}
                      aria-label={`Delete conversation ${c.title}`}
                      /* focus:opacity-100 keeps this reachable by keyboard;
                         opacity alone hid it from anyone not using a mouse. */
                      className="shrink-0 mr-2 p-1.5 rounded text-ink-3 opacity-0 group-hover:opacity-100 focus-visible:opacity-100 hover:text-red-600 hover:bg-mute-bg">
                      <TrashIcon />
                    </button>
                  )}
                </li>
              );
            })}
          </ul>
        </div>
      ))}
    </aside>
  );
}
