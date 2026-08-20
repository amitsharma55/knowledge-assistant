const headers = () => ({ 'X-Dev-Groups': 'everyone' });

export async function listChats() {
  const res = await fetch('/v1/chats', { headers: headers() });
  if (!res.ok) throw new Error('list chats failed');
  return res.json();
}

export async function getMessages(chatId) {
  const res = await fetch(`/v1/chats/${chatId}/messages`, { headers: headers() });
  if (!res.ok) throw new Error('get messages failed');
  return res.json();
}

export async function deleteChat(chatId) {
  const res = await fetch(`/v1/chats/${chatId}`, { method: 'DELETE', headers: headers() });
  if (!res.ok) throw new Error('delete chat failed');
}

export async function uploadFile(file, sessionId, persist) {
  const fd = new FormData();
  fd.append('file', file);
  fd.append('sessionId', sessionId);
  fd.append('persist', persist ? 'true' : 'false');
  const res = await fetch('/v1/uploads', { method: 'POST', headers: headers(), body: fd });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

// streamChat POSTs a message and yields SSE events one at a time.
// Consumer handles { type: 'chat'|'citation'|'token'|'done'|'error', data }.
export async function* streamChat({ chatId, sessionId, message }) {
  const res = await fetch('/v1/chat/messages', {
    method: 'POST',
    headers: { ...headers(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ chatId, sessionId, message }),
  });
  if (!res.ok || !res.body) throw new Error('stream failed');
  const reader = res.body.getReader();
  const dec = new TextDecoder();
  let buf = '';
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
}
