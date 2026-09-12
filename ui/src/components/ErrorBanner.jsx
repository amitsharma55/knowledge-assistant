import { AlertIcon, CloseIcon } from '../icons.jsx';

export default function ErrorBanner({ message, onDismiss }) {
  return (
    <div
      role="alert"
      className="flex items-start gap-2 px-4 py-2 bg-red-50 border-b border-red-200 text-sm text-red-900">
      <AlertIcon className="w-4 h-4 mt-0.5 shrink-0 text-red-600" />
      <p className="flex-1">{message}</p>
      <button
        onClick={onDismiss}
        aria-label="Dismiss this message"
        className="shrink-0 text-red-500 hover:text-red-800 p-1 -m-1">
        <CloseIcon />
      </button>
    </div>
  );
}
