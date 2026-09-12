// Starter questions shown on an empty thread. The teams endpoint returns only
// a slug and a display name, so these live here rather than coming from the
// API -- greeting an HR user with a ServiceNow question was worse than a
// hardcoded map. If the team set stops being fixed, move this server-side.
const BY_TEAM = {
  coupa: [
    'What fields does the ServiceNow integration send?',
    'What Icertis contracts are synced today with Coupa?',
    'List all lambda runs for AVR integration',
  ],
  star: [
    'How do the Texas and Louisiana processing rules differ?',
    'What runs in the data call pipeline, in order?',
    'Which reports are in the Louisiana report catalog?',
  ],
  hr: [
    'How is compensation modelled in Workday?',
    'How does the Workday payroll integration run?',
    'How much leave carries over at year end?',
  ],
};

const FALLBACK = [
  'What does this team own?',
  'Which integrations are documented here?',
  'Who is on call for this system?',
];

export function promptsFor(team) {
  return BY_TEAM[team] ?? FALLBACK;
}
