// @vitest-environment node
import { describe, it, expect, vi, afterEach } from 'vitest';
import { streamChat } from './api';

// A response body that delivers the given strings as separate reads, the way
// SSE frames arrive split across network chunks.
const body = (...reads) => {
  const enc = new TextEncoder();
  return new ReadableStream({
    start(c) {
      for (const r of reads) c.enqueue(enc.encode(r));
      c.close();
    },
  });
};

const collect = async (it) => {
  const out = [];
  for await (const e of it) out.push(e);
  return out;
};

afterEach(() => vi.unstubAllGlobals());

describe('streamChat', () => {
  it('yields one event per SSE frame, reassembling frames split across reads', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      body: body('event: retrieval\ndata: [{"id":"a"}]\n\nevent: tok', 'en\ndata: "Hel"\n\n', 'event: token\ndata: "lo"\n\nevent: done\ndata: {}\n\n'),
    });
    vi.stubGlobal('fetch', fetchMock);

    const events = await collect(streamChat({ chatId: 'c1', message: 'hi', team: 'coupa' }));

    expect(events).toEqual([
      { type: 'retrieval', data: [{ id: 'a' }] },
      { type: 'token', data: 'Hel' },
      { type: 'token', data: 'lo' },
      { type: 'done', data: {} },
    ]);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/v1/chat/messages');
    expect(init.headers['X-Team']).toBe('coupa');
    expect(JSON.parse(init.body)).toEqual({ chatId: 'c1', message: 'hi' });
  });

  it('skips frames with malformed JSON or no data line', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      body: body('event: token\ndata: {bad\n\nevent: ping\n\nevent: token\ndata: "ok"\n\n'),
    }));
    expect(await collect(streamChat({ message: 'hi' }))).toEqual([{ type: 'token', data: 'ok' }]);
  });

  it('throws when the request fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, body: null }));
    await expect(collect(streamChat({ message: 'hi' }))).rejects.toThrow('stream failed');
  });
});

describe('review queue api', () => {
  it('listPending GETs the admin endpoint with the team header', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => [{ id: 'x' }] });
    vi.stubGlobal('fetch', fetchMock);
    const { listPending } = await import('./api');
    const items = await listPending('coupa');
    expect(items).toEqual([{ id: 'x' }]);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/v1/admin/pending');
    expect(init.headers['X-Team']).toBe('coupa');
  });

  it('approvePending POSTs to the approve route', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, text: async () => '' });
    vi.stubGlobal('fetch', fetchMock);
    const { approvePending } = await import('./api');
    await approvePending('abc', 'coupa');
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/v1/admin/pending/abc/approve');
    expect(init.method).toBe('POST');
  });
});
