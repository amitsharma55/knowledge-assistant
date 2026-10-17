import { describe, it, expect } from 'vitest';
import { parseTicket } from './ticket';

describe('parseTicket', () => {
  it('parses a filed confirmation into number + url', () => {
    const t = parseTicket('Filed INC0010028 — https://dev.service-now.com/incident.do?sys_id=abc');
    expect(t).toEqual({ kind: 'filed', number: 'INC0010028', url: 'https://dev.service-now.com/incident.do?sys_id=abc' });
  });

  it('parses a filed confirmation with no url', () => {
    expect(parseTicket('Filed INC0010028')).toEqual({ kind: 'filed', number: 'INC0010028', url: '' });
  });

  it('parses a draft into team, issue, and priority', () => {
    const draft = 'Draft incident for coupa:\n- Issue: Icertis contracts are not syncing\n- Priority: Moderate\n\nReply **confirm** to file it, or **cancel**.';
    expect(parseTicket(draft)).toEqual({
      kind: 'draft', team: 'coupa', issue: 'Icertis contracts are not syncing', priority: 'Moderate',
    });
  });

  it('returns null for prompts, status lines, and normal prose', () => {
    expect(parseTicket("What's the issue you'd like to report? Describe it in one line.")).toBeNull();
    expect(parseTicket('Cancelled — nothing was filed.')).toBeNull();
    expect(parseTicket('AVR sends fields to Coupa via the nightly batch.')).toBeNull();
    expect(parseTicket('')).toBeNull();
    expect(parseTicket(undefined)).toBeNull();
  });
});
