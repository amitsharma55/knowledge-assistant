// groupByDate buckets chats into Today / Yesterday / Previous 7 days / Previous 30 days.
// Assumes input is already sorted most-recent-first.
export function groupByDate(chats) {
  const now = new Date();
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const startOfYesterday = new Date(startOfToday); startOfYesterday.setDate(startOfYesterday.getDate() - 1);
  const startOf7 = new Date(startOfToday); startOf7.setDate(startOf7.getDate() - 7);
  const startOf30 = new Date(startOfToday); startOf30.setDate(startOf30.getDate() - 30);

  const groups = [
    { label: 'Today',              items: [] },
    { label: 'Yesterday',          items: [] },
    { label: 'Previous 7 days',    items: [] },
    { label: 'Previous 30 days',   items: [] },
  ];

  for (const c of chats) {
    const d = new Date(c.updatedAt);
    if (d >= startOfToday)          groups[0].items.push(c);
    else if (d >= startOfYesterday) groups[1].items.push(c);
    else if (d >= startOf7)         groups[2].items.push(c);
    else if (d >= startOf30)        groups[3].items.push(c);
  }
  return groups.filter(g => g.items.length > 0);
}
