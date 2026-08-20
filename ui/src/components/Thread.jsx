import { useEffect, useRef } from 'react';
import Message from './Message.jsx';

const suggestions = [
  'What fields does the ServiceNow integration send?',
  'What Icertis contracts are synced today with Coupa?',
  'List all lambda runs for AVR integration',
];

export default function Thread({ messages, streaming, onSuggest }) {
  const endRef = useRef();
  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [messages]);

  if (messages.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center bg-slate-50">
        <div className="max-w-md text-center">
          <h2 className="text-lg font-semibold text-slate-700 mb-2">Ask about your integrations</h2>
          <p className="text-sm text-slate-500 mb-4">
            Answers come from your Confluence knowledge base. You can also attach a PDF or markdown file.
          </p>
          <div className="flex flex-col gap-2 text-left">
            {suggestions.map(s => (
              <button
                key={s}
                onClick={() => onSuggest(s)}
                className="text-sm px-3 py-2 rounded-md border border-slate-200 bg-white text-slate-700 hover:border-blue-400 hover:text-blue-700">
                {s}
              </button>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto bg-slate-50">
      {messages.map((m, i) => {
        const isLast = i === messages.length - 1;
        return (
          <Message
            key={m.id || i}
            role={m.role}
            content={m.content}
            citations={m.citations}
            streaming={streaming && isLast && m.role === 'assistant'}
          />
        );
      })}
      <div ref={endRef} />
    </div>
  );
}
