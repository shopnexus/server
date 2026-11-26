import { config } from './config.js';
import { profiles } from './profiles.js';

// About how long a visit lasts with its pauses at THINK_SCALE=1. Visits
// started per second times this is how many run at once (Little's law),
// which sizes each scenario's pool of VUs.
const VISIT_SECONDS = { guest: 30, member: 45, trade: 60 };

function shape() {
  const stages = profiles[config.profile];
  if (!stages) throw new Error(`PROFILE=${config.profile}: want one of ${Object.keys(profiles).join(', ')}`);
  return stages;
}

function stages(peak) {
  return shape().map((s) => ({
    duration: `${Math.max(1, Math.round(s.minutes * 60 * config.timeScale))}s`,
    target: Math.round(s.level * peak),
  }));
}

function topLevel() {
  return Math.max(...shape().map((s) => s.level));
}

// visits starts visits at a rate, whether or not earlier ones finished, as
// people arrive on their own (an open model). Rates count per minute, so a
// peak below one visit a second still has steps.
function visits(exec, perSecond) {
  const busy = topLevel() * perSecond * VISIT_SECONDS[exec] * Math.max(config.thinkScale, 0.1);
  const pool = Math.ceil(busy * 1.2) + 1;
  return {
    executor: 'ramping-arrival-rate',
    exec,
    timeUnit: '1m',
    startRate: 0,
    stages: stages(perSecond * 60),
    preAllocatedVUs: pool,
    maxVUs: pool * 2,
  };
}

// sockets keeps one VU per open app, so the VU count is the concurrent users.
function sockets(peak) {
  return { executor: 'ramping-vus', exec: 'socket', startVUs: 0, stages: stages(peak), gracefulRampDown: '30s' };
}

export function buildScenarios() {
  const scenarios = {};
  if (config.guestRate > 0) scenarios.guest = visits('guest', config.guestRate);
  if (config.memberRate > 0) scenarios.member = visits('member', config.memberRate);
  if (config.tradeRate > 0) scenarios.trade = visits('trade', config.tradeRate);
  if (config.sockets > 0) scenarios.socket = sockets(config.sockets);
  if (Object.keys(scenarios).length === 0) throw new Error('GUEST_RATE, MEMBER_RATE, TRADE_RATE and SOCKETS are all 0');
  return scenarios;
}

// totalVUs is the most VUs the scenarios can run at once. VU numbers go up
// to it, and a signed-in VU takes the session with its number.
export function totalVUs(scenarios) {
  return Object.values(scenarios).reduce(
    (sum, s) => sum + (s.maxVUs ?? Math.max(s.startVUs ?? 0, ...s.stages.map((st) => st.target))),
    0,
  );
}
