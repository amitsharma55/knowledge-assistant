const headers = (team) => ({ 'X-Dev-Groups': 'everyone', ...(team ? { 'X-Team': team } : {}) });

export async function listTeams() {
  const res = await fetch('/v1/teams', { headers: headers() });
  if (!res.ok) throw new Error('list teams failed');
  return res.json();
}

export async function listChats(team) {
  const res = await fetch('/v1/chats', { headers: headers(team) });
  if (!res.ok) throw new Error('list chats failed');
  return res.json();
}

export async function getMessages(chatId, team) {
  const res = await fetch(`/v1/chats/${chatId}/messages`, { headers: headers(team) });
  if (!res.ok) throw new Error('get messages failed');
  return res.json();
}

export async function deleteChat(chatId, team) {
  const res = await fetch(`/v1/chats/${chatId}`, { method: 'DELETE', headers: headers(team) });
  if (!res.ok) throw new Error('delete chat failed');
}

export async function uploadFile(file, sessionId, persist, team) {
  const fd = new FormData();
  fd.append('file', file);
  fd.append('sessionId', sessionId);
  fd.append('persist', persist ? 'true' : 'false');
  const res = await fetch('/v1/uploads', { method: 'POST', headers: headers(team), body: fd });
  if (!res.ok) {
    // 422 means the PII gate rejected the document; carry the status so the UI
    // can show the redact-and-retry message rather than a generic failure.
    const err = new Error(await res.text());
    err.status = res.status;
    throw err;
  }
  return res.json();
}

// Admin review queue. listPending returns the team's pending uploads; approve
// promotes one into the index, reject discards it. All are team-scoped.
export async function listPending(team) {
  const res = await fetch('/v1/admin/pending', { headers: headers(team) });
  if (!res.ok) throw new Error('list pending failed');
  return res.json();
}

export async function approvePending(id, team) {
  const res = await fetch(`/v1/admin/pending/${id}/approve`, { method: 'POST', headers: headers(team) });
  if (!res.ok) throw new Error(await res.text());
}

export async function rejectPending(id, team) {
  const res = await fetch(`/v1/admin/pending/${id}/reject`, { method: 'POST', headers: headers(team) });
  if (!res.ok) throw new Error('reject failed');
}

// streamChat POSTs a message and yields SSE events one at a time.
// Consumer handles { type: 'chat'|'retrieval'|'citation'|'token'|'suggestion'|'done'|'error', data }.
// `signal` aborts the request; the caller is expected to swallow the resulting
// AbortError, since a stopped answer is a normal outcome rather than a failure.
export async function* streamChat({ chatId, sessionId, message, team, signal }) {
  const res = await fetch('/v1/chat/messages', {
    method: 'POST',
    headers: { ...headers(team), 'Content-Type': 'application/json' },
    body: JSON.stringify({ chatId, sessionId, message }),
    signal,
  });
  if (!res.ok || !res.body) throw new Error('stream failed');
  const reader = res.body.getReader();
  const dec = new TextDecoder();
  let buf = '';
  try {
    while (true) {
      const { value, done } = await reader.read();
      if (done) return;
      buf += dec.decode(value, { stream: true });
      const frames = buf.split('\n\n');
      buf = frames.pop() || '';
      for (const frame of frames) {
        const type = (frame.split('\n').find(l => l.startsWith('event: ')) || '').slice(7);
        const data = (frame.split('\n').find(l => l.startsWith('data: ')) || '').slice(6);
        if (!type || !data) continue;
        let payload;
        try { payload = JSON.parse(data); } catch { continue; }
        yield { type, data: payload };
      }
    }
  } finally {
    // Releasing the lock lets an aborted body tear down instead of leaking the
    // reader when the consumer stops iterating early.
    reader.cancel().catch(() => {});
  }
}
