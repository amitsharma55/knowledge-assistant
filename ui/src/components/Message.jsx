import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { groupCitations } from '../citations';

export default function Message({ role, content, citations, streaming }) {
  const isUser = role === 'user';
  const sources = isUser ? [] : groupCitations(citations);
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
        {sources.length ? (
          <div className="mt-3 pt-2 border-t border-slate-200 text-xs text-slate-500">
            <div className="mb-1 font-medium text-slate-600">Sources</div>
            <ul className="space-y-1">
              {sources.map((doc, i) => (
                <li key={i}>
                  <a href={doc.url} target="_blank" rel="noreferrer" className="font-medium text-slate-700 underline hover:text-slate-900">
                    {doc.title}
                  </a>
                  <span className="ml-1">
                    {doc.sections.map((s, j) => (
                      <span key={j}>
                        {j > 0 && ' · '}
                        <span className="text-slate-400">{s.numbers.map((n) => `[${n}]`).join('')}</span>
                        {s.label ? ` ${s.label}` : ''}
                      </span>
                    ))}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </div>
    </div>
  );
}
