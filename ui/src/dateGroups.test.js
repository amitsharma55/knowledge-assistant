import { describe, it, expect, vi, afterEach } from 'vitest';
import { groupByDate } from './dateGroups';

const at = (daysAgo, hour) => ({ id: `d${daysAgo}`, updatedAt: new Date(2026, 8, 10 - daysAgo, hour).toISOString() });

afterEach(() => vi.useRealTimers());

describe('groupByDate', () => {
  it('buckets chats by local calendar day and drops anything older than 30 days', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 8, 10, 12, 0));
    const groups = groupByDate([at(0, 9), at(1, 23), at(3, 10), at(20, 10), at(40, 10)]);
    expect(groups.map((g) => [g.label, g.items.map((c) => c.id)])).toEqual([
      ['Today', ['d0']],
      ['Yesterday', ['d1']],
      ['Previous 7 days', ['d3']],
      ['Previous 30 days', ['d20']],
    ]);
  });

  it('omits empty buckets', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 8, 10, 12, 0));
    expect(groupByDate([at(0, 8)]).map((g) => g.label)).toEqual(['Today']);
  });
});
