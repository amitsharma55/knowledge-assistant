import { useEffect, useRef } from 'react';
import Message from './Message.jsx';
import { promptsFor } from '../prompts.js';

export default function Thread({ messages, streaming, retrieving, team, onSuggest, activeChunkId, onSelectChunk, summary }) {
  const endRef = useRef();
  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [messages]);

  if (messages.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center bg-surface p-6">
        <div className="max-w-md text-center">
          <h2 className="font-serif text-xl font-semibold mb-2 text-balance">
            Ask about your integrations
          </h2>
          <p className="text-sm text-ink-2 mb-5">
            Answers come from your team&rsquo;s documentation, and every claim links back to the
            passage behind it. You can also attach a PDF or markdown file.
          </p>
          <ul className="flex flex-col gap-2 text-left">
            {promptsFor(team).map(s => (
              <li key={s}>
                <button
                  onClick={() => onSuggest(s)}
                  className="w-full text-left text-sm px-3 py-2 rounded border border-rule bg-surface
                             text-ink-2 hover:border-accent-edge hover:text-ink">
                  {s}
                </button>
              </li>
            ))}
          </ul>
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto bg-surface" aria-busy={streaming || undefined}>
      {messages.map((m, i) => {
        const isLast = i === messages.length - 1;
        const showSummary = m.role === 'user' && i === messages.length - 2 && summary;
        return (
          <div key={m.id || i}>
            <Message
              role={m.role}
              content={m.content}
              citations={m.citations}
              streaming={streaming && isLast && m.role === 'assistant'}
              retrieving={retrieving && isLast && m.role === 'assistant'}
              activeChunkId={activeChunkId}
              onSelectChunk={onSelectChunk}
            />
            {showSummary && (
              <div className="px-4 sm:px-10">
                <p className="max-w-[46rem] mx-auto font-mono text-[11px] text-ink-3
                              pb-4 border-b border-rule">
                  {summary}
                </p>
              </div>
            )}
          </div>
        );
      })}
      <div ref={endRef} />
    </div>
  );
}
