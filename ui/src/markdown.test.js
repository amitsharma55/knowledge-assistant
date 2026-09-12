import { describe, it, expect } from 'vitest';
import { rehypeCitations } from './markdown.jsx';

const text = (value) => ({ type: 'text', value });
const el = (tagName, children, properties = {}) => ({ type: 'element', tagName, properties, children });
const cite = (n) => el('citeref', [text(n)], { 'data-n': n });

const run = (tree) => {
  rehypeCitations()(tree);
  return tree;
};

describe('rehypeCitations', () => {
  it('turns [n] markers in text into citeref elements', () => {
    const tree = run({ type: 'root', children: [el('p', [text('See [1] and [12].')])] });
    expect(tree.children[0].children).toEqual([text('See '), cite('1'), text(' and '), cite('12'), text('.')]);
  });

  it('leaves code, pre and links alone, since [n] there is not a marker', () => {
    const untouched = [el('code', [text('arr[0]')]), el('pre', [text('[1]')]), el('a', [text('[2]')], { href: 'x' })];
    const tree = run({ type: 'root', children: structuredClone(untouched) });
    expect(tree.children).toEqual(untouched);
  });

  it('ignores brackets that are not markers, including a marker still streaming in', () => {
    const tree = run({ type: 'root', children: [el('p', [text('a [x] b [')]), el('p', [text('[1234]')])] });
    expect(tree.children[0].children).toEqual([text('a [x] b [')]);
    expect(tree.children[1].children).toEqual([text('[1234]')]);
  });
});
