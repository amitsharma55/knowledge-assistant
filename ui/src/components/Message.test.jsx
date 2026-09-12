import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, fireEvent, cleanup } from '@testing-library/react';
import Message from './Message.jsx';

afterEach(cleanup);

const citations = [
  { id: 'chunk-a', pageId: 'p1', pageTitle: 'AVR SOAP Service', url: 'https://example.test/avr', sectionPath: 'Operations' },
  { pageId: 'p1', pageTitle: 'AVR SOAP Service', url: 'https://example.test/avr', sectionPath: 'Errors' },
];

const answer = (props) =>
  render(<Message role="assistant" content="Use the push operation [1]. Failures are logged [2]." citations={citations} {...props} />);

describe('Message', () => {
  it('renders [n] markers as buttons that select the cited chunk', () => {
    const onSelectChunk = vi.fn();
    answer({ onSelectChunk });
    fireEvent.click(screen.getByRole('button', { name: 'Show source 1' }));
    expect(onSelectChunk).toHaveBeenCalledWith('chunk-a');
  });

  it('disables a marker whose citation has no chunk id', () => {
    answer({ onSelectChunk: vi.fn() });
    expect(screen.getByRole('button', { name: 'Show source 2' }).disabled).toBe(true);
  });

  it('marks the marker for the active chunk as pressed', () => {
    answer({ activeChunkId: 'chunk-a', onSelectChunk: vi.fn() });
    expect(screen.getByRole('button', { name: 'Show source 1' }).getAttribute('aria-pressed')).toBe('true');
  });

  it('lists sources once per document with each section labelled', () => {
    answer({ onSelectChunk: vi.fn() });
    const link = screen.getByRole('link', { name: 'AVR SOAP Service' });
    expect(link.closest('li').textContent).toBe('AVR SOAP Service[1] Operations · [2] Errors');
  });

  it('shows the searching state before any answer text arrives', () => {
    render(<Message role="assistant" content="" retrieving citations={[]} />);
    expect(screen.getByText(/Searching your team/)).toBeTruthy();
  });

  it('renders the user turn as the question heading', () => {
    render(<Message role="user" content="How does the AVR sync retry?" />);
    expect(screen.getByRole('heading', { name: 'How does the AVR sync retry?' })).toBeTruthy();
  });
});
