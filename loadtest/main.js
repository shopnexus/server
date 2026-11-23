// The ShopNexus load test. README.md says how to run it; lib/config.js lists
// the settings.
import exec from 'k6/execution';
import { discover } from './lib/catalog.js';
import { buildScenarios, totalVUs } from './lib/scenarios.js';
import { sessionCount } from './lib/session.js';
import { thresholds } from './lib/thresholds.js';
import { guestVisit } from './journeys/guest.js';
import { memberVisit } from './journeys/member.js';
import { holdSocket } from './journeys/socket.js';

const scenarios = buildScenarios();

export const options = {
  scenarios,
  thresholds,
  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
  setupTimeout: '2m',
};

export function setup() {
  const needed = totalVUs(scenarios);
  if ((scenarios.member || scenarios.socket) && sessionCount() < needed) {
    exec.test.abort(`the run can have ${needed} VUs and each signed-in VU needs its own session, ` +
      `but there are ${sessionCount()}: go run ./cmd/loadsessions -sessions ${needed}`);
  }
  return discover();
}

export function guest(catalog) {
  guestVisit(catalog);
}

export function member(catalog) {
  memberVisit(catalog);
}

export function socket() {
  holdSocket();
}
