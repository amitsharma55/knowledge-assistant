import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

export default function Message({ role, content, citations, streaming }) {
  const isUser = role === 'user';
  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'} px-6 py-3`}>
      <div className={`max-w-3xl ${isUser ? 'bg-blue-600 text-white' : 'bg-white text-slate-900 border border-slate-200'} rounded-lg px-4 py-3 shadow-sm`}>
        {content || streaming ? (
          <div className={`prose prose-sm max-w-none ${isUser ? 'prose-invert' : ''}`}>
            <ReactMarkdown remarkPlugins={[remarkGfm]}>
              {content || (streaming ? '…' : '')}
            </ReactMarkdown>
          </div>
        ) : null}
        {citations?.length ? (
          <div className="mt-3 pt-2 border-t border-slate-200 text-xs text-slate-500">
            <span className="mr-1">Sources:</span>
            {citations.map((c, i) => (
              <span key={i}>
                {i > 0 && ' · '}
                <a href={c.url} target="_blank" rel="noreferrer" className="underline hover:text-slate-700">
                  [{i + 1}] {c.pageTitle}
                </a>
              </span>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}
