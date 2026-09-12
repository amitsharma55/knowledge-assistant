import { describe, it, expect } from 'vitest';
import { promptsFor } from './prompts';

describe('promptsFor', () => {
  it('returns the team-specific starter questions', () => {
    expect(promptsFor('hr')).toContain('How is compensation modelled in Workday?');
  });

  it('falls back to generic questions for an unknown team', () => {
    const fallback = promptsFor('unknown-team');
    expect(fallback).toHaveLength(3);
    for (const team of ['coupa', 'star', 'hr']) expect(fallback).not.toEqual(promptsFor(team));
  });
});
