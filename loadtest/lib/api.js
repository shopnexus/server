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
    return this.request('PUT', route, { expect: [204], ...opts });
  }

  del(route, opts) {
    return this.request('DELETE', route, { expect: [204], ...opts });
  }

  // request sends method to route, filling its {names} from path. k6 drops
  // response bodies (main.js) unless read is set. A signed-in request that
  // gets 401 refreshes the session and tries once more.
  request(method, route, { path = {}, query = {}, body, expect = [200], read = false } = {}) {
    const url = config.baseUrl + fill(route, path) + queryString(query);
    const payload = body === undefined ? null : JSON.stringify(body);
    if (this.session) this.session.ensureFresh();
    let res = http.request(method, url, payload, this.params(route, expect, read));
    if (res.status === 401 && this.session) {
      this.session.refresh();
      res = http.request(method, url, payload, this.params(route, expect, read));
    }
    check(res, { [`${method} ${route} answers ${expect.join(' or ')}`]: (r) => expect.includes(r.status) });
    return res;
  }

  params(route, expect, read) {
    const headers = { Accept: 'application/json', 'Content-Type': 'application/json', ...addressHeaders() };
    if (this.session) headers.Authorization = `Bearer ${this.session.accessToken}`;
    return {
      headers,
      tags: { name: route, journey: this.journey },
      responseCallback: expected(expect),
      responseType: read ? 'text' : 'none',
    };
  }
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
