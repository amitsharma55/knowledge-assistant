import { useEffect, useRef, useState } from 'react';
import TopBar from './components/TopBar.jsx';
import Sidebar from './components/Sidebar.jsx';
import Thread from './components/Thread.jsx';
import Composer from './components/Composer.jsx';
import { listChats, getMessages, deleteChat, uploadFile, streamChat } from './api.js';

export default function App() {
  const [chats, setChats] = useState([]);
  const [currentChatId, setCurrentChatId] = useState(null);
  const [messages, setMessages] = useState([]);
  const [uploads, setUploads] = useState([]);
  const [busy, setBusy] = useState(false);
  const sessionId = useRef(crypto.randomUUID());

  const currentChat = chats.find(c => c.id === currentChatId);

  useEffect(() => { refreshChats(); }, []);
  useEffect(() => {
    if (!currentChatId) { setMessages([]); return; }
    getMessages(currentChatId).then(setMessages).catch(() => setMessages([]));
  }, [currentChatId]);

  async function refreshChats() {
    try { setChats(await listChats()); } catch {}
  }

  function newChat() {
    setCurrentChatId(null);
    setMessages([]);
    setUploads([]);
    sessionId.current = crypto.randomUUID();
  }

  async function onDelete(id) {
    await deleteChat(id);
    if (id === currentChatId) newChat();
    refreshChats();
  }

  async function onUpload(file, persist) {
    setBusy(true);
    try {
      const info = await uploadFile(file, sessionId.current, persist);
      setUploads(u => [...u, info]);
    } catch (e) {
      console.error(e);
    } finally {
      setBusy(false);
    }
  }

  async function send(text) {
    setBusy(true);
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
      })) {
        if (ev.type === 'chat') {
          createdChatId = ev.data.chatId;
          setCurrentChatId(createdChatId);
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
        } else if (ev.type === 'error') {
          setMessages(m => {
            const last = { ...m[m.length - 1], content: 'Error: ' + ev.data };
            return [...m.slice(0, -1), last];
          });
        }
      }
    } finally {
      setBusy(false);
      refreshChats();
    }
  }

  return (
    <div className="flex flex-col h-full">
      <TopBar title={currentChat?.title || 'New chat'} onNewChat={newChat} />
      <div className="flex flex-1 min-h-0">
        <Sidebar
          chats={chats}
          currentChatId={currentChatId}
          onSelect={setCurrentChatId}
          onDelete={onDelete}
        />
        <main className="flex-1 flex flex-col min-w-0">
          <Thread messages={messages} streaming={busy} onSuggest={send} />
          <Composer
            onSend={send}
            onUpload={onUpload}
            busy={busy}
            uploads={uploads}
            onRemoveUpload={id => setUploads(u => u.filter(x => x.uploadId !== id))}
          />
        </main>
      </div>
    </div>
  );
}
