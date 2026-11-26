import http from 'k6/http';
import exec from 'k6/execution';
import { SharedArray } from 'k6/data';
import { Counter } from 'k6/metrics';
import { config } from './config.js';
import { addressHeaders } from './address.js';

// Refresh this long before the access token expires (it lasts 15 minutes).
const REFRESH_MARGIN_MS = 60 * 1000;

const lost = new Counter('sessions_lost');

// What cmd/loadsessions printed: the sessions of the member and socket VUs,
// and the trades of the trade VUs. open() resolves from this file, so a
// relative SESSIONS_FILE is taken from loadtest/.
// SharedArray reads the file once, not once per VU.
const read = (part) => (needsSessions() ? JSON.parse(open(fromLoadtest(config.sessionsFile)))[part] ?? [] : []);
const entries = new SharedArray('sessions', () => read('sessions'));
const trades = new SharedArray('trades', () => read('trades'));

function needsSessions() {
  return config.memberRate > 0 || config.sockets > 0 || config.tradeRate > 0;
}

function fromLoadtest(path) {
  return path.startsWith('/') ? path : `../${path.replace(/^\.\//, '')}`;
}

// sessionCount is how many sessions the file holds, tradeCount how many trades.
export function sessionCount() {
  return entries.length;
}

export function tradeCount() {
  return trades.length;
}

// A Session is one signed-in device. It rotates its own refresh token, so no
// two VUs may share one: a refresh token presented twice signs the session out.
export class Session {
  constructor(entry) {
    this.accountId = entry.account_id;
    this.accessToken = entry.access_token;
    this.refreshToken = entry.refresh_token;
    this.expiresAt = Date.parse(entry.access_token_expires_at);
  }

  ensureFresh() {
    if (Date.now() > this.expiresAt - REFRESH_MARGIN_MS) this.refresh();
  }

  refresh() {
    const res = http.post(`${config.baseUrl}/v1/auth/refresh`, JSON.stringify({ refresh_token: this.refreshToken }), {
      headers: { 'Content-Type': 'application/json', ...addressHeaders() },
      tags: { name: '/v1/auth/refresh', journey: 'auth' },
    });
    if (res.status !== 200) {
      lost.add(1);
      throw new Error(`refresh for account ${this.accountId}: ${res.status} ${res.body}`);
    }
    const tokens = res.json();
    this.accessToken = tokens.access_token;
    this.refreshToken = tokens.refresh_token;
    this.expiresAt = Date.parse(tokens.access_token_expires_at);
  }
}

let mine;

// vuSession is this VU's session: entry number idInTest of the file, kept for
// every iteration the VU runs.
export function vuSession() {
  if (!mine) {
    const entry = entries[exec.vu.idInTest - 1];
    if (!entry) {
      exec.test.abort(`VU ${exec.vu.idInTest} has no session: ${config.sessionsFile} holds ${entries.length}`);
    }
    mine = new Session(entry);
  }
  return mine;
}

let myTrade;

// vuTrade is this VU's trade, entry number idInTest of the file's trades:
// its buyer and seller sessions and the listing they deal on.
export function vuTrade() {
  if (!myTrade) {
    const entry = trades[exec.vu.idInTest - 1];
    if (!entry) {
      exec.test.abort(`VU ${exec.vu.idInTest} has no trade: ${config.sessionsFile} holds ${trades.length}`);
    }
    myTrade = { buyer: new Session(entry.buyer), seller: new Session(entry.seller), listingId: entry.listing_id };
  }
  return myTrade;
}
