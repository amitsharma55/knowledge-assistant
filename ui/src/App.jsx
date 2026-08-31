import { useEffect, useRef, useState } from 'react';
import TopBar from './components/TopBar.jsx';
import Sidebar from './components/Sidebar.jsx';
import Thread from './components/Thread.jsx';
import Composer from './components/Composer.jsx';
import ContextPanel from './components/ContextPanel.jsx';
import { listTeams, listChats, getMessages, deleteChat, uploadFile, streamChat } from './api.js';

export default function App() {
  const [teams, setTeams] = useState([]);
  const [team, setTeam] = useState(null);
  const [chats, setChats] = useState([]);
  const [currentChatId, setCurrentChatId] = useState(null);
  const [messages, setMessages] = useState([]);
  const [uploads, setUploads] = useState([]);
  const [busy, setBusy] = useState(false);
  const [retrieval, setRetrieval] = useState([]);
  const [panelOpen, setPanelOpen] = useState(true);
  const [suggestions, setSuggestions] = useState([]);
  const sessionId = useRef(crypto.randomUUID());
  // streamingRef guards the message-fetch effect during send(). Without it, the
  // `chat` SSE event for a newly-created chat triggers setCurrentChatId, which
  // fires the effect below and re-fetches messages from Postgres — clobbering
  // our optimistic [user, assistant-placeholder] state so subsequent token
  // events append to the wrong turn.
  const streamingRef = useRef(false);

  const currentChat = chats.find(c => c.id === currentChatId);

  useEffect(() => {
    listTeams().then(ts => { setTeams(ts); setTeam(ts[0]?.slug ?? null); }).catch(() => setTeams([]));
  }, []);

  useEffect(() => {
    if (!team) return;
    newChat();
    refreshChats();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [team]);

  useEffect(() => {
    if (streamingRef.current) return;
    if (!team || !currentChatId) { setMessages([]); return; }
    getMessages(currentChatId, team).then(setMessages).catch(() => setMessages([]));
  }, [currentChatId, team]);

  async function refreshChats() {
    if (!team) return;
    try { setChats(await listChats(team)); } catch {}
  }

  function newChat() {
    setCurrentChatId(null);
    setMessages([]);
    setUploads([]);
    setRetrieval([]);
    setSuggestions([]);
    sessionId.current = crypto.randomUUID();
  }

  async function onDelete(id) {
    await deleteChat(id, team);
    if (id === currentChatId) newChat();
    refreshChats();
  }

  async function onUpload(file, persist) {
    setBusy(true);
    try {
      const info = await uploadFile(file, sessionId.current, persist, team);
      setUploads(u => [...u, info]);
    } catch (e) {
      console.error(e);
    } finally {
      setBusy(false);
    }
  }

  async function send(text) {
    streamingRef.current = true;
    setBusy(true);
    setRetrieval([]);
    setSuggestions([]);
    // Optimistically append user turn + a placeholder assistant turn.
    setMessages(m => [
      ...m,
      { role: 'user', content: text },
      { role: 'assistant', content: '', citations: [] },
    ]);
    let createdChatId = currentChatId;
    try {
      for await (const ev of streamChat({
        chatId: currentChatId,
        sessionId: sessionId.current,
        message: text,
        team,
      })) {
        if (ev.type === 'chat') {
          createdChatId = ev.data.chatId;
          setCurrentChatId(createdChatId);
        } else if (ev.type === 'retrieval') {
          setRetrieval(ev.data);
        } else if (ev.type === 'citation') {
          setMessages(m => {
            const last = { ...m[m.length - 1], citations: ev.data };
            return [...m.slice(0, -1), last];
          });
        } else if (ev.type === 'token') {
          setMessages(m => {
            const last = { ...m[m.length - 1], content: (m[m.length - 1].content || '') + ev.data };
            return [...m.slice(0, -1), last];
          });
        } else if (ev.type === 'suggestion') {
          setSuggestions(ev.data);
        } else if (ev.type === 'error') {
          setMessages(m => {
            const last = { ...m[m.length - 1], content: 'Error: ' + ev.data };
            return [...m.slice(0, -1), last];
          });
        }
      }
    } finally {
      streamingRef.current = false;
      setBusy(false);
      refreshChats();
    }
  }

  function teamDisplayName(slug) {
    return teams.find(t => t.slug === slug)?.displayName ?? slug;
  }

  return (
    <div className="flex flex-col h-full">
      <TopBar
        title={currentChat?.title || 'New chat'}
        onNewChat={newChat}
        teams={teams}
        team={team}
        onTeamChange={setTeam}
      />
      <div className="flex flex-1 min-h-0">
        <Sidebar
          chats={chats}
          currentChatId={currentChatId}
          onSelect={setCurrentChatId}
          onDelete={onDelete}
        />
        <main className="flex-1 flex flex-col min-w-0">
          <Thread messages={messages} streaming={busy} onSuggest={send} />
          {suggestions.length > 0 && (
            <div className="flex flex-wrap gap-2 px-3 pt-2">
              {suggestions.map(s => (
                <button key={s.team} onClick={() => setTeam(s.team)}
                  className="text-xs px-2 py-1 rounded border border-amber-300 bg-amber-50 text-amber-900">
                  No results here — {s.matches} match{s.matches === 1 ? '' : 'es'} in {teamDisplayName(s.team)}. Switch?
                </button>
              ))}
            </div>
          )}
          <Composer
            onSend={send}
            onUpload={onUpload}
            busy={busy || !team}
            uploads={uploads}
            onRemoveUpload={id => setUploads(u => u.filter(x => x.uploadId !== id))}
          />
        </main>
        <ContextPanel chunks={retrieval} open={panelOpen} onToggle={() => setPanelOpen(o => !o)} />
      </div>
    </div>
  );
}
