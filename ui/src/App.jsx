import { useEffect, useRef, useState } from 'react';
import TopBar from './components/TopBar.jsx';
import Sidebar from './components/Sidebar.jsx';
import Thread from './components/Thread.jsx';
import Composer from './components/Composer.jsx';
import ContextPanel from './components/ContextPanel.jsx';
import ErrorBanner from './components/ErrorBanner.jsx';
import { citationTargets, groupCitations } from './citations.js';
import { listTeams, listChats, getMessages, deleteChat, uploadFile, streamChat } from './api.js';
import { uuid } from './uuid.js';

export default function App() {
  const [teams, setTeams] = useState([]);
  const [team, setTeam] = useState(null);
  const [chats, setChats] = useState([]);
  const [currentChatId, setCurrentChatId] = useState(null);
  const [messages, setMessages] = useState([]);
  const [uploads, setUploads] = useState([]);
  // Three states that used to share one `busy` flag. Conflating them meant an
  // upload put a streaming cursor on the last answer and locked the team
  // switcher, and streaming locked the textarea so the next question could not
  // be drafted while the current one arrived.
  const [streaming, setStreaming] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [retrieving, setRetrieving] = useState(false);
  const [error, setError] = useState(null);
  const [retrieval, setRetrieval] = useState([]);
  // Per-viewer convenience: someone who works with the rail shut should not
  // have to shut it again on every question. Reads can throw outright in a
  // locked-down browser, so both sides are guarded.
  const [panelOpen, setPanelOpen] = useState(() => {
    try { return localStorage.getItem('ka.panel') !== 'closed'; } catch { return true; }
  });
  // The passage currently linked to the answer. Set from either side: a [n] in
  // the answer, or a row in the rail.
  const [activeChunkId, setActiveChunkId] = useState(null);
  const [suggestions, setSuggestions] = useState([]);
  const sessionId = useRef(uuid());
  // streamingRef guards the message-fetch effect during send(). Without it, the
  // `chat` SSE event for a newly-created chat triggers setCurrentChatId, which
  // fires the effect below and re-fetches messages from Postgres — clobbering
  // our optimistic [user, assistant-placeholder] state so subsequent token
  // events append to the wrong turn.
  const streamingRef = useRef(false);
  const abortRef = useRef(null);

  const currentChat = chats.find(c => c.id === currentChatId);

  useEffect(() => {
    listTeams()
      .then(ts => {
        setTeams(ts);
        setTeam(ts[0]?.slug ?? null);
        if (ts.length === 0) setError('No teams are configured. Check that chat-api is running.');
      })
      .catch(() => {
        setTeams([]);
        setError('Could not reach chat-api. Start it with `make run-api`, then reload.');
      });
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
    getMessages(currentChatId, team)
      .then(setMessages)
      .catch(() => { setMessages([]); setError('Could not load that conversation.'); });
  }, [currentChatId, team]);

  async function refreshChats() {
    if (!team) return;
    try { setChats(await listChats(team)); } catch {}
  }

  function newChat() {
    stop();
    setCurrentChatId(null);
    setMessages([]);
    setUploads([]);
    setRetrieval([]);
    setSuggestions([]);
    setError(null);
    setActiveChunkId(null);
    sessionId.current = uuid();
  }

  async function onDelete(id) {
    try {
      await deleteChat(id, team);
    } catch {
      setError('Could not delete that conversation.');
      return;
    }
    if (id === currentChatId) newChat();
    refreshChats();
  }

  async function onUpload(file, persist) {
    setUploading(true);
    setError(null);
    try {
      const info = await uploadFile(file, sessionId.current, persist, team);
      setUploads(u => [...u, info]);
    } catch (e) {
      setError(`Could not attach ${file.name}. ${e.message || 'The upload failed.'}`);
    } finally {
      setUploading(false);
    }
  }

  // stop aborts an in-flight answer. Whatever tokens already arrived stay on
  // screen — a stopped answer is a partial answer, not a discarded one.
  function stop() {
    abortRef.current?.abort();
    abortRef.current = null;
  }

  async function send(text) {
    streamingRef.current = true;
    setStreaming(true);
    setRetrieving(true);
    setError(null);
    setRetrieval([]);
    setSuggestions([]);
    setActiveChunkId(null);
    const ctrl = new AbortController();
    abortRef.current = ctrl;
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
        signal: ctrl.signal,
      })) {
        if (ev.type === 'chat') {
          createdChatId = ev.data.chatId;
          setCurrentChatId(createdChatId);
        } else if (ev.type === 'retrieval') {
          setRetrieval(ev.data);
          setRetrieving(false);
        } else if (ev.type === 'citation') {
          setMessages(m => {
            const last = { ...m[m.length - 1], citations: ev.data };
            return [...m.slice(0, -1), last];
          });
        } else if (ev.type === 'token') {
          setRetrieving(false);
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
    } catch (e) {
      // An abort is the stop button doing its job, not a failure to report.
      if (e.name !== 'AbortError') {
        setError('The answer stopped early. ' + (e.message || 'The connection failed.'));
      }
    } finally {
      abortRef.current = null;
      streamingRef.current = false;
      setStreaming(false);
      setRetrieving(false);
      refreshChats();
    }
  }

  function togglePanel() {
    setPanelOpen(o => {
      const next = !o;
      try { localStorage.setItem('ka.panel', next ? 'open' : 'closed'); } catch {}
      return next;
    });
  }

  // Selecting a passage from a closed rail has to open it, or the click on the
  // citation looks like it did nothing.
  function selectChunk(id) {
    setActiveChunkId(cur => (cur === id ? null : id));
    if (!panelOpen) {
      setPanelOpen(true);
      try { localStorage.setItem('ka.panel', 'open'); } catch {}
    }
  }

  // citedNumbers inverts the citation list so a rail row can show which [n]
  // markers point at it. Built from the newest assistant turn, which is the
  // one the retrieval on screen belongs to.
  const lastAssistant = [...messages].reverse().find(m => m.role === 'assistant');
  const citedNumbers = new Map();
  citationTargets(lastAssistant?.citations).forEach((chunkId, n) => {
    if (!citedNumbers.has(chunkId)) citedNumbers.set(chunkId, []);
    citedNumbers.get(chunkId).push(n);
  });

  const sentCount = retrieval.filter(c => c.used).length;
  const docCount = groupCitations(lastAssistant?.citations).length;
  const summary = retrieval.length
    ? `${retrieval.length} passage${retrieval.length === 1 ? '' : 's'} retrieved · ` +
      `${sentCount} sent to the model` + (docCount ? ` · ${docCount} document${docCount === 1 ? '' : 's'} cited` : '')
    : null;

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
        streaming={streaming}
      />
      {error && <ErrorBanner message={error} onDismiss={() => setError(null)} />}
      <div className="flex flex-1 min-h-0">
        <Sidebar
          chats={chats}
          currentChatId={currentChatId}
          onSelect={setCurrentChatId}
          onDelete={onDelete}
        />
        <main className="flex-1 flex flex-col min-w-0">
          <Thread
            messages={messages}
            streaming={streaming}
            retrieving={retrieving}
            team={team}
            onSuggest={send}
            activeChunkId={activeChunkId}
            onSelectChunk={selectChunk}
            summary={summary}
          />
          {suggestions.length > 0 && (
            <div className="flex flex-wrap gap-2 px-3 pt-2">
              {suggestions.map(s => (
                <button key={s.team} onClick={() => setTeam(s.team)}
                  className="text-xs px-2 py-1 rounded border border-amber-300 bg-amber-50 text-amber-900 hover:bg-amber-100">
                  No results here — {s.matches} match{s.matches === 1 ? '' : 'es'} in {teamDisplayName(s.team)}. Switch?
                </button>
              ))}
            </div>
          )}
          <Composer
            onSend={send}
            onUpload={onUpload}
            onStop={stop}
            streaming={streaming}
            uploading={uploading}
            disabled={!team}
            uploads={uploads}
            onRemoveUpload={id => setUploads(u => u.filter(x => x.uploadId !== id))}
          />
        </main>
        <ContextPanel
          chunks={retrieval}
          retrieving={retrieving}
          open={panelOpen}
          onToggle={togglePanel}
          activeChunkId={activeChunkId}
          onSelectChunk={selectChunk}
          citedNumbers={citedNumbers}
        />
      </div>
    </div>
  );
}
