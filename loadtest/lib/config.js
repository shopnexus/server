// Settings, from `k6 run -e KEY=value` or the environment. The defaults are a
// smoke test against `make run` on this machine.

function text(key, fallback) {
  const raw = __ENV[key];
  return raw === undefined || raw === '' ? fallback : raw;
}

function number(key, fallback) {
  const raw = text(key, '');
  if (raw === '') return fallback;
  const n = Number(raw);
  if (!Number.isFinite(n) || n < 0) throw new Error(`${key}=${raw}: want a number of 0 or more`);
  return n;
}

function flag(key) {
  const raw = text(key, 'false');
  if (raw !== 'true' && raw !== 'false') throw new Error(`${key}=${raw}: want true or false`);
  return raw === 'true';
}

const baseUrl = text('BASE_URL', 'http://127.0.0.1:8080').replace(/\/+$/, '');

export const config = Object.freeze({
  baseUrl,
  wsUrl: baseUrl.replace(/^http/, 'ws'),
  // A shape of profiles.js.
  profile: text('PROFILE', 'smoke'),
  // Peak load, which each profile's stages scale: guest, member and trade
  // visits started per second, and open sockets (signed-in users with the
  // app open). A trade chats in words, so the stack needs
  // TRUST_SCAM_CHECKS=off, or each message is a paid model call.
  guestRate: number('GUEST_RATE', 1),
  memberRate: number('MEMBER_RATE', 0),
  tradeRate: number('TRADE_RATE', 0),
  sockets: number('SOCKETS', 0),
  // Stretches every profile's stages; 0.1 runs a load test in a tenth.
  timeScale: number('TIME_SCALE', 1),
  // Multiplies every pause between steps; 0 sends the steps back to back.
  thinkScale: number('THINK_SCALE', 1),
  // How long a socket stays open before the app "closes" and reopens it.
  socketMinutes: number('SOCKET_MINUTES', 10),
  // What cmd/loadsessions printed, relative to main.js.
  sessionsFile: text('SESSIONS_FILE', './sessions.json'),
  // Sends each VU's requests from its own address in 198.18.0.0/15 through
  // X-Forwarded-For. The server reads that header only from an address in
  // its TRUSTED_PROXIES.
  spreadAddresses: flag('SPREAD_ADDRESSES'),
});
