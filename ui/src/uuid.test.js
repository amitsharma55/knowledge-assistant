import { describe, it, expect, vi, afterEach } from 'vitest';
import { uuid } from './uuid.js';

const V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

afterEach(() => vi.unstubAllGlobals());

describe('uuid', () => {
  it('produces a v4 UUID', () => {
    expect(uuid()).toMatch(V4);
  });

  it('works with no crypto.randomUUID (the insecure http:// context)', () => {
    // getRandomValues is available on insecure origins; randomUUID is not.
    vi.stubGlobal('crypto', {
      getRandomValues: (a) => { for (let i = 0; i < a.length; i++) a[i] = (i * 37) & 0xff; return a; },
    });
    expect(uuid()).toMatch(V4);
  });

  it('falls back when crypto is entirely absent', () => {
    vi.stubGlobal('crypto', undefined);
    expect(uuid()).toMatch(V4);
  });

  it('produces distinct values', () => {
    expect(uuid()).not.toBe(uuid());
  });
});
