export default function ContextPanel({ chunks, open, onToggle }) {
  if (!chunks?.length) return null;
  const used = chunks.filter((c) => c.used);
  const dropped = chunks.filter((c) => !c.used);

  return (
    <aside className={`${open ? 'w-96' : 'w-10'} border-l border-slate-200 bg-slate-50 flex flex-col transition-all`}>
      <button
        onClick={onToggle}
        className="h-10 shrink-0 text-xs text-slate-500 hover:text-slate-800 border-b border-slate-200">
        {open ? 'Hide context' : '‹'}
      </button>
      {open && (
        <div className="flex-1 overflow-y-auto p-3 space-y-3">
          <p className="text-xs font-medium text-slate-500 uppercase tracking-wide">
            Passed to the model ({used.length})
          </p>
          {/* The score is vector similarity, computed before the question was
              compared to any chunk. Ordering and selection are the reranker's.
              Without saying so, a 0.83 sitting under "not used" next to a 0.79
              that was sent reads as a bug rather than as the reranker working. */}
          <p className="text-[11px] text-slate-500 leading-snug">
            Ordered by rerank position. The score is vector similarity from
            retrieval, not the reranker&rsquo;s judgement, so it does not decide
            what is sent.
          </p>
          {used.map((c) => <ChunkCard key={c.id} chunk={c} />)}
          {dropped.length > 0 && (
            <>
              <p className="text-xs font-medium text-slate-400 uppercase tracking-wide pt-2 border-t border-slate-200">
                Retrieved, not used ({dropped.length})
              </p>
              {dropped.map((c) => <ChunkCard key={c.id} chunk={c} muted />)}
            </>
          )}
        </div>
      )}
    </aside>
  );
}

const REASON = {
  selected: { label: 'selected', title: 'Top-ranked by the reranker', className: 'bg-emerald-100 text-emerald-800' },
  backfilled: { label: 'backfilled', title: 'A sibling section of a page already selected, added to keep the document whole', className: 'bg-sky-100 text-sky-800' },
  dropped: { label: 'dropped', title: 'Ranked below the cut', className: 'bg-slate-100 text-slate-500' },
};

function ChunkCard({ chunk, muted }) {
  const reason = REASON[chunk.reason];
  return (
    <div className={`rounded-md border border-slate-200 bg-white p-2 ${muted ? 'opacity-60' : ''}`}>
      <div className="flex items-center gap-2 mb-1">
        <span className="text-[10px] uppercase tracking-wide px-1.5 py-0.5 rounded bg-slate-900 text-white">
          {chunk.team}
        </span>
        {chunk.rank ? (
          <span className="text-[11px] text-slate-600 tabular-nums" title="Position after reranking">
            #{chunk.rank}
          </span>
        ) : null}
        {reason ? (
          <span className={`text-[10px] px-1.5 py-0.5 rounded ${reason.className}`} title={reason.title}>
            {reason.label}
          </span>
        ) : null}
        <span className="text-[11px] text-slate-400 tabular-nums ml-auto" title="Vector similarity from retrieval">
          {chunk.score?.toFixed(3)}
        </span>
      </div>
      <a href={chunk.url} target="_blank" rel="noreferrer"
         className="text-xs font-medium text-slate-800 hover:underline block truncate">
        {chunk.pageTitle}
      </a>
      {chunk.sectionPath && (
        <div className="text-[11px] text-slate-500 truncate">{chunk.sectionPath}</div>
      )}
      <p className="text-[11px] text-slate-600 mt-1 line-clamp-6 whitespace-pre-wrap">{chunk.text}</p>
    </div>
  );
}
