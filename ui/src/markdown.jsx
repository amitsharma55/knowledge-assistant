// rehypeCitations turns the model's [1] / [2] markers into real elements, so
// they can be rendered as buttons that select the passage behind them.
//
// This runs on the HTML tree rather than on the markdown string because `[1]`
// is also link syntax: rewriting the raw text would mangle any genuine
// reference link. Working on text nodes after parsing means a marker is only
// ever a marker.
//
// During streaming the accumulated content is re-parsed on every token, so a
// marker split across two tokens ("[" then "1]") renders as plain text for one
// frame and becomes a button as soon as the closing bracket arrives.
const MARKER = /\[(\d{1,3})\]/g;
const SKIP = new Set(['code', 'pre', 'a']);

export function rehypeCitations() {
  return (tree) => walk(tree);
}

function walk(node) {
  if (!node || !Array.isArray(node.children)) return;
  if (node.type === 'element' && SKIP.has(node.tagName)) return;

  const out = [];
  let changed = false;
  for (const child of node.children) {
    if (child.type !== 'text' || !child.value.includes('[')) {
      walk(child);
      out.push(child);
      continue;
    }
    const parts = split(child.value);
    if (parts.length === 1 && parts[0].type === 'text') {
      out.push(child);
      continue;
    }
    changed = true;
    out.push(...parts);
  }
  if (changed) node.children = out;
}

function split(value) {
  const parts = [];
  let last = 0;
  MARKER.lastIndex = 0;
  let m;
  while ((m = MARKER.exec(value)) !== null) {
    if (m.index > last) parts.push({ type: 'text', value: value.slice(last, m.index) });
    parts.push({
      type: 'element',
      tagName: 'citeref',
      properties: { 'data-n': m[1] },
      children: [{ type: 'text', value: m[1] }],
    });
    last = m.index + m[0].length;
  }
  if (last === 0) return [{ type: 'text', value }];
  if (last < value.length) parts.push({ type: 'text', value: value.slice(last) });
  return parts;
}
