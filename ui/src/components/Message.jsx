import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { groupCitations, citationTargets } from '../citations';
import { rehypeCitations } from '../markdown.jsx';
import { parseTicket } from '../ticket.js';
import { TicketIcon, CheckCircleIcon } from '../icons.jsx';

// TicketCard renders the create flow's deterministic messages (a draft to
// confirm, or a filed incident) as a compact card rather than serif prose: a
// ticket is a record, not a passage, and the book layout made it read like one.
function TicketCard({ ticket }) {
  if (ticket.kind === 'filed') {
    return (
      <div className="rounded-lg border border-rule bg-surface-2 px-4 py-3 not-prose">
        <div className="flex items-center gap-2 text-good text-sm font-medium">
          <CheckCircleIcon className="w-4 h-4" />
          Incident filed
        </div>
        <div className="mt-1.5 font-mono text-[15px]">
          {ticket.url ? (
            <a href={ticket.url} target="_blank" rel="noreferrer"
               className="text-accent underline underline-offset-2 decoration-accent-edge">
              {ticket.number}
            </a>
          ) : (
            <span>{ticket.number}</span>
          )}
        </div>
      </div>
    );
  }
  return (
    <div className="rounded-lg border border-accent-edge bg-accent-tint/40 px-4 py-3 not-prose">
      <div className="flex items-center gap-2 text-accent text-xs font-semibold uppercase tracking-wide">
        <TicketIcon className="w-4 h-4" />
        Draft incident{ticket.team ? ` · ${ticket.team}` : ''}
      </div>
      <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
        <dt className="text-ink-3">Issue</dt>
        <dd className="text-ink">{ticket.issue}</dd>
        <dt className="text-ink-3">Priority</dt>
        <dd className="text-ink">{ticket.priority}</dd>
      </dl>
      <p className="mt-2.5 pt-2.5 border-t border-accent-edge/50 text-xs text-ink-2">
        Reply <span className="font-semibold text-ink">confirm</span> to file it, or{' '}
        <span className="font-semibold text-ink">cancel</span>.
      </p>
    </div>
  );
}

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
  const ticket = parseTicket(content);

  if (ticket) {
    return (
      <div className="px-4 sm:px-10 pt-4 pb-2">
        <div className="max-w-[46rem] mx-auto">
          <TicketCard ticket={ticket} />
        </div>
      </div>
    );
  }

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
