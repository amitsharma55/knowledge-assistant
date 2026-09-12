import { useEffect, useRef } from 'react';
import { ChevronLeftIcon } from '../icons.jsx';

const REASON = {
  selected: { label: 'selected', title: 'Top-ranked by the reranker', className: 'bg-good-bg text-good' },
  backfilled: { label: 'backfilled', title: 'A sibling section of a page already selected, added to keep the document whole', className: 'bg-info-bg text-info' },
  dropped: { label: 'dropped', title: 'Ranked below the cut', className: 'bg-mute-bg text-mute' },
};

export default function ContextPanel({ chunks, retrieving, open, onToggle, activeChunkId, onSelectChunk, citedNumbers }) {
  const used = chunks?.filter((c) => c.used) ?? [];
  const dropped = chunks?.filter((c) => !c.used) ?? [];
  const empty = !chunks?.length;

  return (
    // Always mounted and always occupying a width -- either the full rail or
    // the collapsed strip. It used to unmount whenever retrieval was empty, so
    // every question collapsed the rail to nothing and slammed it back.
    <aside
      aria-label="Retrieved passages"
      className={`${open ? 'w-[19rem]' : 'w-11'} shrink-0 border-l border-rule bg-paper
                  flex flex-col transition-[width] duration-200`}>
      {open ? (
        <>
          <div className="shrink-0 px-3.5 pt-4 pb-3 border-b border-rule bg-paper">
            <div className="flex items-center gap-2 mb-1.5">
              <h2 className="text-xs font-semibold">Where this answer came from</h2>
              <button
                onClick={onToggle}
                aria-expanded="true"
                aria-controls="provenance-body"
                className="ml-auto flex items-center gap-1 text-[11px] text-ink-3 hover:text-ink
                           hover:bg-surface-2 rounded px-1.5 py-0.5">
                <ChevronLeftIcon className="w-3 h-3 rotate-180" />
                Hide
              </button>
            </div>
            <p className="text-[11px] leading-snug text-ink-3">
              Ordered by rerank position. The score is vector similarity from retrieval, not the
              reranker&rsquo;s judgement, so it does not decide what is sent.
            </p>
            <div className="flex gap-2.5 flex-wrap mt-2.5">
              <Key className="bg-good" label="selected" />
              <Key className="bg-info" label="backfilled" />
              <Key className="bg-mute" label="dropped" />
            </div>
          </div>

          <div id="provenance-body" className="flex-1 overflow-y-auto">
            {retrieving ? (
              <SkeletonList />
            ) : empty ? (
              <p className="p-3.5 text-[11px] leading-snug text-ink-3">
                Ask a question and the passages behind the answer appear here, in the order the
                reranker put them.
              </p>
            ) : (
              <>
                {used.map((c) => (
                  <ChunkRow key={c.id} chunk={c} active={c.id === activeChunkId}
                            numbers={citedNumbers.get(c.id)} onSelect={onSelectChunk} />
                ))}
                {dropped.length > 0 && (
                  <>
                    <p className="font-mono text-[10px] text-ink-3 px-3.5 pt-3.5 pb-1.5
                                  border-t border-rule mt-2">
                      Retrieved, not used ({dropped.length})
                    </p>
                    {dropped.map((c) => (
                      <ChunkRow key={c.id} chunk={c} active={c.id === activeChunkId}
                                numbers={citedNumbers.get(c.id)} onSelect={onSelectChunk} muted />
                    ))}
                  </>
                )}
              </>
            )}
          </div>
        </>
      ) : (
        // Collapsed, the strip still reports how many passages backed the
        // answer -- a bare chevron told the reader nothing.
        <button
          onClick={onToggle}
          aria-expanded="false"
          aria-controls="provenance-body"
          className="flex-1 w-full flex flex-col items-center gap-3 pt-3.5 text-ink-3
                     hover:bg-surface-2 hover:text-ink">
          <ChevronLeftIcon className="w-3 h-3" />
          <span className="font-mono text-[10px] tracking-wider [writing-mode:vertical-rl]">
            Sources
          </span>
          {!empty && (
            <span className="font-mono text-[10px] tabular-nums bg-mute-bg text-ink-2 rounded px-1 py-0.5">
              {chunks.length}
            </span>
          )}
        </button>
      )}
    </aside>
  );
}

function Key({ className, label }) {
  return (
    <span className="font-mono text-[10px] flex items-center gap-1 text-ink-3">
      <i className={`w-[7px] h-[7px] rounded-[1px] ${className}`} />
      {label}
    </span>
  );
}

function ChunkRow({ chunk, active, numbers, onSelect, muted }) {
  const reason = REASON[chunk.reason];
  const ref = useRef(null);

  // Following a citation scrolls its passage into view. Only the active row
  // does this, and only when it is not already in frame.
  useEffect(() => {
    if (!active || !ref.current) return;
    ref.current.scrollIntoView({
      behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth',
      block: 'nearest',
    });
  }, [active]);

  return (
    <button
      ref={ref}
      onClick={() => onSelect(chunk.id)}
      aria-pressed={active ? 'true' : 'false'}
      className={`grid grid-cols-[2.1rem_minmax(0,1fr)] w-full text-left px-3 py-2.5
                  border-b border-rule ${active ? 'bg-accent-tint' : 'hover:bg-surface-2'}`}>
      {/* Rank has its own gutter, so vertical position carries the ordering. */}
      <span className={`font-mono text-[11px] tabular-nums pt-px
                        ${active ? 'text-accent font-semibold' : 'text-ink-3'}`}>
        {chunk.rank || '·'}
      </span>
      <span className="min-w-0">
        <span className="flex items-center gap-1.5 mb-1 flex-wrap">
          {reason && (
            <span className={`font-mono text-[10px] px-1.5 py-px rounded-sm ${reason.className}`}
                  title={reason.title}>
              {reason.label}
            </span>
          )}
          {numbers?.length ? (
            <span className="font-mono text-[10px] text-accent">
              {numbers.map((n) => `[${n}]`).join('')}
            </span>
          ) : null}
          <span className="font-mono text-[10px] tabular-nums text-ink-3 ml-auto"
                title="Vector similarity from retrieval">
            {chunk.score?.toFixed(3)}
          </span>
        </span>
        <span className={`block text-xs font-medium leading-snug truncate
                          ${muted ? 'text-ink-3' : 'text-ink'}`}>
          {chunk.pageTitle}
        </span>
        {chunk.sectionPath && (
          <span className="block font-mono text-[10px] text-ink-3 truncate mb-1">
            {chunk.sectionPath}
          </span>
        )}
        <span className={`block text-[11.5px] leading-snug line-clamp-4 whitespace-pre-wrap
                          ${muted ? 'text-ink-3' : 'text-ink-2'}`}>
          {chunk.text}
        </span>
      </span>
    </button>
  );
}

function SkeletonList() {
  return (
    <div aria-hidden="true">
      {[0, 1, 2].map((i) => (
        <div key={i} className="px-3 py-2.5 border-b border-rule animate-pulse">
          <div className="flex items-center gap-2 mb-2">
            <div className="h-3.5 w-14 rounded bg-rule" />
            <div className="h-3.5 w-7 rounded bg-rule/60" />
          </div>
          <div className="h-3 w-3/4 rounded bg-rule mb-2" />
          <div className="space-y-1">
            <div className="h-2.5 w-full rounded bg-rule/60" />
            <div className="h-2.5 w-full rounded bg-rule/60" />
            <div className="h-2.5 w-2/3 rounded bg-rule/60" />
          </div>
        </div>
      ))}
    </div>
  );
}
