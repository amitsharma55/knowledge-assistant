import { useState } from 'react';
import { AlertIcon } from '../icons.jsx';

const RESTRICTED = [
  ['PCI', 'Payment card data — card numbers, CVV, cardholder details.'],
  ['PHI (HIPAA)', 'Protected health information — diagnoses, treatment, insurance, medical records.'],
  ['PII', 'Personally identifiable information — names tied to SSNs, government IDs, financial accounts.'],
  ['PI', 'Personal information — any data that identifies or can be linked to an individual.'],
];

// Full-screen acknowledgement shown over everything until accepted. No
// persistence: a fresh page load is a fresh acknowledgement, which is what a
// per-session data-handling notice wants. OK stays disabled until the user
// checks the box, so acceptance is an explicit act rather than a reflex click.
export default function DisclaimerGate({ onAccept }) {
  const [confirmed, setConfirmed] = useState(false);

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="disclaimer-title"
      className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/60 p-4">
      <div className="max-w-lg w-full rounded-xl bg-white shadow-2xl">
        <div className="flex items-center gap-3 rounded-t-xl border-b border-slate-200 bg-slate-50 px-6 py-4">
          <AlertIcon className="w-6 h-6 shrink-0 text-amber-500" />
          <div>
            <h2 id="disclaimer-title" className="text-base font-semibold text-slate-900">
              Acceptable Use &amp; Data Handling Notice
            </h2>
            <p className="text-xs text-slate-500">Please review before continuing</p>
          </div>
        </div>

        <div className="px-6 py-5 text-sm text-slate-700">
          <p>
            This assistant is provided for authorized business use. To protect
            our customers and comply with data-protection obligations,
            <span className="font-medium text-slate-900"> do not enter or upload any restricted data</span>,
            including:
          </p>
          <ul className="mt-4 space-y-2">
            {RESTRICTED.map(([tag, desc]) => (
              <li key={tag} className="flex gap-3">
                <span className="mt-0.5 inline-flex shrink-0 items-center rounded bg-slate-100 px-2 py-0.5 text-xs font-semibold text-slate-700">
                  {tag}
                </span>
                <span className="text-slate-600">{desc}</span>
              </li>
            ))}
          </ul>
          <p className="mt-4 text-xs text-slate-500">
            Conversations may be logged and reviewed for quality, security, and
            compliance. Responses may be inaccurate — verify before relying on
            them for business decisions.
          </p>

          <label className="mt-5 flex items-start gap-2.5 text-sm text-slate-800">
            <input
              type="checkbox"
              checked={confirmed}
              onChange={e => setConfirmed(e.target.checked)}
              className="mt-0.5 h-4 w-4 rounded border-slate-300 text-blue-600 focus:ring-blue-500"
            />
            <span>
              I understand and will not submit PCI, PHI, PII, or other personal
              or restricted information.
            </span>
          </label>
        </div>

        <div className="flex justify-end rounded-b-xl border-t border-slate-200 bg-slate-50 px-6 py-4">
          <button
            onClick={onAccept}
            disabled={!confirmed}
            className="rounded-md bg-blue-600 px-5 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-slate-300">
            Accept &amp; Continue
          </button>
        </div>
      </div>
    </div>
  );
}
