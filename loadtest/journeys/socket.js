import { WebSocket } from 'k6/websockets';
import { Counter, Rate, Trend } from 'k6/metrics';
import { addressHeaders } from '../lib/address.js';
import { config } from '../lib/config.js';
import { vuSession } from '../lib/session.js';

const ready = new Trend('socket_ready', true);
const updates = new Counter('socket_updates');
const dropped = new Rate('socket_dropped');

// How often the open app checks whether its access token needs renewing.
const RENEW_CHECK_MS = 30 * 1000;

// An open app: one WebSocket, which the server answers with a ready frame,
// held for about SOCKET_MINUTES. The access token is renewed over the socket
// before it expires, as the app does. Then the app closes, and the next
// iteration opens it again, so each VU is one concurrent user.
export function holdSocket() {
  const session = vuSession();
  session.ensureFresh();
  const opened = Date.now();
  let leaving = false;
  const ws = new WebSocket(`${config.wsUrl}/v1/ws`, null, {
    headers: { Authorization: `Bearer ${session.accessToken}`, ...addressHeaders() },
    tags: { name: '/v1/ws', journey: 'socket' },
  });

  const renew = setInterval(() => {
    const before = session.accessToken;
    session.ensureFresh();
    if (session.accessToken !== before) ws.send(JSON.stringify({ type: 'auth', token: session.accessToken }));
  }, RENEW_CHECK_MS);
  // Spread the closes, so the VUs do not all reconnect at once.
  const hold = config.socketMinutes * 60 * 1000 * (0.8 + Math.random() * 0.4);
  const leave = setTimeout(() => {
    leaving = true;
    ws.close();
  }, hold);

  ws.onmessage = (event) => {
    const frame = JSON.parse(event.data);
    if (frame.type === 'ready') ready.add(Date.now() - opened);
    else if (frame.type === 'update') updates.add(1);
  };
  ws.onclose = () => {
    clearInterval(renew);
    clearTimeout(leave);
    dropped.add(!leaving);
  };
}
