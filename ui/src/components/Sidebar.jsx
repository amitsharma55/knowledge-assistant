import { groupByDate } from '../dateGroups.js';

export default function Sidebar({ chats, currentChatId, onSelect, onDelete }) {
  const groups = groupByDate(chats);
  return (
    <aside className="w-64 border-r border-slate-200 bg-slate-50 overflow-y-auto">
      {groups.length === 0 && (
        <div className="p-4 text-sm text-slate-400">No chats yet.</div>
      )}
      {groups.map(group => (
        <div key={group.label} className="pt-3">
          <div className="px-3 pb-1 text-[11px] font-medium uppercase tracking-wide text-slate-500">
            {group.label}
          </div>
          {group.items.map(c => (
            <div
              key={c.id}
              className={
                'group flex items-center justify-between px-3 py-1.5 cursor-pointer text-sm ' +
                (c.id === currentChatId
                  ? 'bg-slate-200 text-slate-900'
                  : 'text-slate-700 hover:bg-slate-100')
              }
              onClick={() => onSelect(c.id)}>
              <span className="truncate">{c.title}</span>
              <button
                title="Delete chat"
                onClick={e => { e.stopPropagation(); onDelete(c.id); }}
                className="opacity-0 group-hover:opacity-100 text-slate-400 hover:text-red-600 ml-2">
                ×
              </button>
            </div>
          ))}
        </div>
      ))}
    </aside>
  );
}
