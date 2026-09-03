// groupCitations turns the flat, per-chunk citation list into one entry per
// document, each listing the sections cited from it.
//
// Every chunk was rendered as its own link labelled with the page title, so
// five sections of one page produced five identical "avr field mapping"
// links, which told the reader nothing about which part of the document a
// claim came from. Grouping by page and labelling by section fixes that.
//
// The [n] markers are the model's own references into the context blocks, so
// they are carried through unchanged and never renumbered. A section split
// across several chunks collects all of its numbers under one label.
export function groupCitations(citations = []) {
  const docs = new Map();
  citations.forEach((c, i) => {
    const n = i + 1;
    const docKey = c.pageId || c.url || c.pageTitle || String(n);
    if (!docs.has(docKey)) {
      docs.set(docKey, { title: c.pageTitle || 'Untitled', url: c.url, sections: new Map() });
    }
    const doc = docs.get(docKey);
    // A section whose path just repeats the page title adds nothing; label it
    // by its numbers alone rather than printing the title twice.
    const label = c.sectionPath && c.sectionPath !== doc.title ? c.sectionPath : '';
    const secKey = label || ' ';
    if (!doc.sections.has(secKey)) doc.sections.set(secKey, { label, numbers: [] });
    doc.sections.get(secKey).numbers.push(n);
  });
  return [...docs.values()].map((d) => ({ ...d, sections: [...d.sections.values()] }));
}
