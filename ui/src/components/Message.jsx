import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { groupCitations, citationTargets } from '../citations';
import { rehypeCitations } from '../markdown.jsx';

// The user's turn is set as the question that heads a section, and the
// assistant's as the document under it. Neither is a bubble: long answers full
// of field lists and code spans read better as prose than inside a card, and
// the old right-aligned blue bubble made the question the loudest thing on a
// screen whose subject is the answer.
export default function Message({ role, content, citations, streaming, retrieving, activeChunkId, onSelectChunk }) {
  if (role === 'user') {
    return (
      <div className="px-4 sm:px-10 pt-8">
        <div className="max-w-[46rem] mx-auto">
          <p className="text-xs text-ink-3 mb-1.5">You asked</p>
          <h2 className="font-serif text-[22px] leading-tight font-semibold text-balance tracking-tight">
            {content}
          </h2>
        </div>
      </div>
    );
  }

  const sources = groupCitations(citations);
  const targets = citationTargets(citations);

  return (
    <div className="px-4 sm:px-10 pt-4 pb-2">
      <div className="max-w-[46rem] mx-auto">
        {retrieving && !content ? (
          <p className="text-sm text-ink-3 flex items-center gap-2 py-2">
            <span className="w-3 h-3 rounded-full border-2 border-rule-2 border-t-ink-3 animate-spin" />
            Searching your team&rsquo;s documentation…
          </p>
        ) : content || streaming ? (
          <div className="answer-prose prose max-w-none">
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              rehypePlugins={[rehypeCitations]}
              components={{
                citeref: ({ node, ...props }) => {
                  const n = Number(props['data-n']);
                  const chunkId = targets.get(n);
                  const on = chunkId && chunkId === activeChunkId;
                  return (
                    <button
                      type="button"
                      onClick={() => chunkId && onSelectChunk(chunkId)}
                      disabled={!chunkId}
                      aria-label={`Show source ${n}`}
                      aria-pressed={on ? 'true' : 'false'}
                      className={
                        'font-mono text-[0.72em] font-medium align-super px-px underline ' +
                        'underline-offset-2 decoration-1 text-accent ' +
                        (chunkId ? 'cursor-pointer hover:bg-accent-tint ' : 'cursor-default ') +
                        (on ? 'bg-accent-tint ring-2 ring-accent-tint' : '')
                      }>
                      {n}
                    </button>
                  );
                },
              }}>
              {content || (streaming ? '…' : '')}
            </ReactMarkdown>
          </div>
        ) : null}

        {sources.length ? (
          <div className="mt-6 pt-3.5 border-t border-rule text-xs text-ink-2">
            <h3 className="text-xs font-semibold mb-2">Sources</h3>
            <ul className="space-y-1">
              {sources.map((doc, i) => (
                <li key={i}>
                  <a href={doc.url} target="_blank" rel="noreferrer"
                     className="text-ink underline underline-offset-2">
                    {doc.title}
                  </a>
                  <span className="ml-1.5 font-mono text-[11px] text-ink-3">
                    {doc.sections.map((s, j) => (
                      <span key={j}>
                        {j > 0 && ' · '}
                        {s.numbers.map((n) => `[${n}]`).join('')}
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
