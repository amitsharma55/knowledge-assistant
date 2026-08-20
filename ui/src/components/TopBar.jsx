export default function TopBar({ title, onNewChat }) {
  return (
    <header className="h-14 border-b border-slate-200 bg-white flex items-center px-4 gap-4">
      <div className="flex items-center gap-2">
        <div className="w-8 h-8 rounded-md bg-slate-900 text-white flex items-center justify-center font-semibold">
          KA
        </div>
        <span className="font-semibold text-slate-800">Knowledge Assistant</span>
      </div>
      <div className="flex-1 text-sm text-slate-500 truncate">{title}</div>
      <button
        onClick={onNewChat}
        className="px-3 py-1.5 text-sm rounded-md border border-slate-300 hover:bg-slate-50">
        + New chat
      </button>
    </header>
  );
}
