export default function TopBar({ title, onNewChat, teams, team, onTeamChange, streaming }) {
  return (
    <header className="h-14 border-b border-rule bg-surface flex items-center px-4 gap-4">
      <div className="flex items-center gap-2 shrink-0">
        <div className="w-8 h-8 rounded-md bg-ink text-white flex items-center justify-center font-semibold">
          KA
        </div>
        <span className="font-semibold text-ink">Knowledge Assistant</span>
      </div>
      <label className="flex items-center gap-2 shrink-0">
        <span className="sr-only">Team</span>
        <select
          value={team ?? ''}
          onChange={e => onTeamChange(e.target.value)}
          /* Only streaming locks this: switching teams mid-answer would leave
             the retrieved chunks scoped to a team the user has left. An upload
             has no such conflict, so it no longer disables the switcher. */
          disabled={streaming}
          className="text-sm border border-rule-2 rounded-md px-2 py-1.5 bg-surface disabled:opacity-50 disabled:cursor-not-allowed">
          {teams.length === 0 && <option value="">No teams</option>}
          {teams.map(t => <option key={t.slug} value={t.slug}>{t.displayName}</option>)}
        </select>
      </label>
      <h1 className="flex-1 text-sm text-ink-3 truncate font-normal min-w-0">{title}</h1>
      <button
        onClick={onNewChat}
        className="shrink-0 px-3 py-1.5 text-sm rounded-md border border-rule-2 hover:bg-surface-2">
        New chat
      </button>
    </header>
  );
}
