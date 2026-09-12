import { describe, it, expect } from 'vitest';
import { groupCitations, citationTargets } from './citations';

const chunk = (o) => ({ pageId: 'p1', pageTitle: 'AVR Field Mapping', url: 'u1', sectionPath: 'Fields', ...o });

describe('groupCitations', () => {
  it('groups by page, labels by section, and keeps the model 1-based numbers', () => {
    const out = groupCitations([
      chunk({ id: 'a' }),
      chunk({ id: 'b', pageId: 'p2', pageTitle: 'SOAP Service', url: 'u2', sectionPath: 'Operations' }),
      chunk({ id: 'c', sectionPath: 'Errors' }),
      chunk({ id: 'd' }),
    ]);
    expect(out).toEqual([
      { title: 'AVR Field Mapping', url: 'u1', sections: [{ label: 'Fields', numbers: [1, 4] }, { label: 'Errors', numbers: [3] }] },
      { title: 'SOAP Service', url: 'u2', sections: [{ label: 'Operations', numbers: [2] }] },
    ]);
  });

  it('drops a section label that only repeats the page title', () => {
    const out = groupCitations([chunk({ sectionPath: 'AVR Field Mapping' })]);
    expect(out[0].sections).toEqual([{ label: '', numbers: [1] }]);
  });

  it('keys documents by url when there is no pageId, and titles untitled pages', () => {
    const out = groupCitations([{ url: 'u9' }, { url: 'u9' }, {}]);
    expect(out).toHaveLength(2);
    expect(out[0]).toEqual({ title: 'Untitled', url: 'u9', sections: [{ label: '', numbers: [1, 2] }] });
  });

  it('returns an empty list when there are no citations', () => {
    expect(groupCitations()).toEqual([]);
  });
});

describe('citationTargets', () => {
  it('maps each 1-based marker to its chunk id and skips entries without one', () => {
    expect([...citationTargets([{ id: 'a' }, {}, { id: 'c' }])]).toEqual([[1, 'a'], [3, 'c']]);
  });
});
