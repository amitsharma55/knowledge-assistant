import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react';
import AdminPanel from './AdminPanel.jsx';
import * as api from '../api.js';

beforeEach(() => {
  vi.spyOn(api, 'listPending').mockResolvedValue([
    { id: 'x', filename: 'policy.pdf', uploader: 'dev@example.com', findings: [] },
  ]);
  vi.spyOn(api, 'approvePending').mockResolvedValue();
  vi.spyOn(api, 'rejectPending').mockResolvedValue();
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

describe('AdminPanel', () => {
  it('lists pending items for the team', async () => {
    render(<AdminPanel team="coupa" />);
    await screen.findByText(/policy\.pdf/); // throws if never rendered
    expect(api.listPending).toHaveBeenCalledWith('coupa');
  });

  it('approves an item', async () => {
    render(<AdminPanel team="coupa" />);
    await screen.findByText(/policy\.pdf/);
    fireEvent.click(screen.getByText('Approve'));
    await waitFor(() => expect(api.approvePending).toHaveBeenCalledWith('x', 'coupa'));
  });
});
