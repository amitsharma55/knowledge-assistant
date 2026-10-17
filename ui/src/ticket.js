// parseTicket turns a deterministic ticket message from the assistant into a
// descriptor the UI renders as a card instead of book-style prose. Returns null
// for anything that isn't one (normal answers, the issue/severity prompts,
// status lines like "Cancelled…"), which then render as prose.
//
// The shapes mirror the backend create flow's constant strings
// (internal/rag/ticket.go: draftText and the "Filed …" reply). They are parsed
// here rather than carried as structured stream data because a reloaded chat
// only has the persisted message text — detecting on the text keeps the card
// identical on first stream and on reload.
export function parseTicket(content) {
  if (typeof content !== 'string') return null;
  const text = content.trim();

  // "Filed INC0010028 — https://…"  (em dash from the backend)
  const filed = text.match(/^Filed\s+(INC\d+)(?:\s+[—-]\s+(\S+))?/);
  if (filed) return { kind: 'filed', number: filed[1], url: filed[2] || '' };

  // "Draft incident for <team>:\n- Issue: …\n- Priority: …\n\nReply **confirm**…"
  if (text.startsWith('Draft incident for ')) {
    const team = (text.match(/^Draft incident for\s+(.+?):/) || [])[1] || '';
    const issue = (text.match(/Issue:\s*(.+)/) || [])[1] || '';
    const priority = (text.match(/Priority:\s*(.+)/) || [])[1] || '';
    return { kind: 'draft', team: team.trim(), issue: issue.trim(), priority: priority.trim() };
  }
  return null;
}
