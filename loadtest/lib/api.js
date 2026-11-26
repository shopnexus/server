import http from 'k6/http';
import { check } from 'k6';
import { config } from './config.js';
import { addressHeaders } from './address.js';

// One response callback per set of expected statuses; k6 advises against
// building them per request.
const callbacks = new Map();

function expected(statuses) {
  const key = statuses.join(',');
  if (!callbacks.has(key)) callbacks.set(key, http.expectedStatuses(...statuses));
  return callbacks.get(key);
}

function fill(route, path) {
  return route.replace(/\{(\w+)\}/g, (_, name) => {
    if (path[name] === undefined) throw new Error(`${route}: no value for {${name}}`);
    return encodeURIComponent(path[name]);
  });
}

function queryString(query) {
  const parts = Object.entries(query)
    .filter(([, v]) => v !== undefined && v !== null && v !== '')
    .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(v)}`);
  return parts.length ? `?${parts.join('&')}` : '';
}

// An Api sends one journey's requests, signed in when it has a session. Each
// request is tagged with its route as api/openapi.yaml writes it, so k6's
// results group the way the server's http_route does.
export class Api {
  constructor(journey, session) {
    this.journey = journey;
    this.session = session;
  }

  get(route, opts) {
    return this.request('GET', route, opts);
  }

  put(route, opts) {
    return this.request('PUT', route, opts);
  }

  del(route, opts) {
    return this.request('DELETE', route, opts);
  }

  post(route, opts) {
    return this.request('POST', route, opts);
  }

  // request sends method to route, filling its {names} from path. k6 drops
  // response bodies unless read is set. A signed-in request that gets 401
  // refreshes the session and tries once more. With idempotent set it sends
  // a new Idempotency-Key, the same on the retry.
  request(method, route, opts = {}) {
    const r = this.prepare(method, route, opts);
    let res = http.request(method, r.url, r.payload, this.params(route, r));
    if (res.status === 401 && this.session) {
      this.session.refresh();
      res = http.request(method, r.url, r.payload, this.params(route, r));
    }
    return this.checked(method, route, r, res);
  }

  // send is request without blocking the VU's event loop, so a socket's
  // frames are read while the request is out.
  async send(method, route, opts = {}) {
    const r = this.prepare(method, route, opts);
    let res = await http.asyncRequest(method, r.url, r.payload, this.params(route, r));
    if (res.status === 401 && this.session) {
      this.session.refresh();
      res = await http.asyncRequest(method, r.url, r.payload, this.params(route, r));
    }
    return this.checked(method, route, r, res);
  }

  prepare(method, route, { path = {}, query = {}, body, expect, read = false, idempotent = false }) {
    if (this.session) this.session.ensureFresh();
    return {
      url: config.baseUrl + fill(route, path) + queryString(query),
      payload: body === undefined ? null : JSON.stringify(body),
      expect: expect ?? (method === 'PUT' || method === 'DELETE' ? [204] : [200]),
      read,
      key: idempotent ? uuid() : undefined,
    };
  }

  checked(method, route, r, res) {
    check(res, { [`${method} ${route} answers ${r.expect.join(' or ')}`]: (x) => r.expect.includes(x.status) });
    return res;
  }

  params(route, { expect, read, key }) {
    const headers = { Accept: 'application/json', 'Content-Type': 'application/json', ...addressHeaders() };
    if (this.session) headers.Authorization = `Bearer ${this.session.accessToken}`;
    if (key) headers['Idempotency-Key'] = key;
    return {
      headers,
      tags: { name: route, journey: this.journey },
      responseCallback: expected(expect),
      responseType: read ? 'text' : 'none',
    };
  }
}

// uuid is a random version 4 UUID.
function uuid() {
  const b = crypto.getRandomValues(new Uint8Array(16));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = [...b].map((x) => x.toString(16).padStart(2, '0')).join('');
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

// body parses a JSON response, or gives null for a failed or empty one.
export function body(res) {
  if (res.status < 200 || res.status >= 300 || !res.body) return null;
  try {
    return res.json();
  } catch (_) {
    return null;
  }
}
