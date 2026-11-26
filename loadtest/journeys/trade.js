import { WebSocket } from 'k6/websockets';
import { Rate, Trend } from 'k6/metrics';
import { Api, body } from '../lib/api.js';
import { addressHeaders } from '../lib/address.js';
import { config } from '../lib/config.js';
import { vuTrade } from '../lib/session.js';
import { pick } from '../lib/think.js';

// From the buyer's request to the seller's socket frame about it.
const dealDelivery = new Trend('deal_delivery', true);
const messageDelivery = new Trend('message_delivery', true);
// A frame the seller should have had and did not within DELIVERY_MS.
const missed = new Rate('trade_missed');

const DELIVERY_MS = 10_000;

const buyerLines = ['Mình hỏi chút, hàng còn không bạn?', 'Bạn bớt thêm được không?', 'Mình ở gần, chiều nay qua xem được không?', 'Còn bảo hành không bạn?'];
const sellerLines = ['Còn bạn nhé.', 'Giá đó hơi thấp, mình để thêm chút được không?', 'Bạn qua lúc 5h nhé.', 'Hàng dùng kỹ, còn mới lắm.'];

// pause waits like think, without blocking the event loop the socket reads on.
function pause(min, max) {
  const s = (min + Math.random() * (max - min)) * config.thinkScale;
  return new Promise((resolve) => setTimeout(resolve, s * 1000));
}

// Inbox is the seller's open app: its socket, and the update frames it got,
// each with the time it came. Every timer it sets is cleared once settled:
// k6 ends an iteration only when no timer is left.
class Inbox {
  constructor(session) {
    this.frames = [];
    this.waiters = [];
    this.ws = new WebSocket(`${config.wsUrl}/v1/ws`, null, {
      headers: { Authorization: `Bearer ${session.accessToken}`, ...addressHeaders() },
      tags: { name: '/v1/ws', journey: 'trade' },
    });
    this.ready = new Promise((resolve) => {
      const timer = setTimeout(() => resolve(false), DELIVERY_MS);
      const settle = (ok) => {
        clearTimeout(timer);
        resolve(ok);
      };
      this.ws.onclose = () => settle(false);
      this.ws.onmessage = (event) => {
        const frame = JSON.parse(event.data);
        if (frame.type === 'ready') settle(true);
        if (frame.type !== 'update') return;
        const at = Date.now();
        this.frames.push({ frame, at });
        this.waiters = this.waiters.filter((w) => {
          if (!w.match(frame)) return true;
          w.done(at);
          return false;
        });
      };
    });
  }

  // delivered records how long after since the frame match picks came, or
  // a miss after DELIVERY_MS. A frame that came before the call counts.
  async delivered(match, since, trend) {
    const seen = this.frames.find((f) => f.at >= since && match(f.frame));
    const at = seen ? seen.at : await new Promise((resolve) => {
      const w = { match };
      const timer = setTimeout(() => {
        this.waiters = this.waiters.filter((x) => x !== w);
        resolve(null);
      }, DELIVERY_MS);
      w.done = (t) => {
        clearTimeout(timer);
        resolve(t);
      };
      this.waiters.push(w);
    });
    missed.add(at === null);
    if (at !== null) trend.add(at - since);
  }

  close() {
    this.ws.close();
  }
}

// amountIn is a multiple of 1,000 inside an offer range.
function amountIn(range) {
  const steps = Math.floor((range.max - range.min) / 1000);
  return Math.ceil(range.min / 1000) * 1000 + Math.floor(Math.random() * (steps + 1)) * 1000;
}

// tradeVisit is a buyer and a seller of the load test's own accounts on one
// of the seller's listings: the buyer opens the deal and offers, writes, the
// seller reads and replies, and declines the offer, so the next visit finds
// the deal open again. The seller's app is open throughout, and the visit
// times how long its socket takes to hear of the offer and of the message.
export async function tradeVisit() {
  const trade = vuTrade();
  const buyer = new Api('trade', trade.buyer);
  const seller = new Api('trade', trade.seller);
  trade.seller.ensureFresh();
  const inbox = new Inbox(trade.seller);
  try {
    if (!(await inbox.ready)) {
      missed.add(true);
      return;
    }
    let deal = body(await buyer.send('POST', '/v1/deals', { body: { listing_id: trade.listingId }, expect: [200, 201], read: true }));
    if (!deal) return;
    // A visit cut short left a price open: clear it as the side it waits on.
    if (deal.open_price?.by === 'buyer') {
      deal = body(await buyer.send('POST', '/v1/deals/{id}/withdraw', { path: { id: deal.id }, read: true })) ?? deal;
    } else if (deal.open_price?.by === 'seller') {
      deal = body(await buyer.send('POST', '/v1/deals/{id}/decline', { path: { id: deal.id }, read: true })) ?? deal;
    }
    if (!deal.offer_range) return;
    await pause(3, 8);

    let since = Date.now();
    const offered = await buyer.send('POST', '/v1/deals/{id}/offer', { path: { id: deal.id }, body: { amount: amountIn(deal.offer_range) } });
    if (offered.status === 200) await inbox.delivered((f) => f.kind === 'deal' && f.deal_id === deal.id, since, dealDelivery);
    await pause(2, 6);

    since = Date.now();
    const sent = body(await buyer.send('POST', '/v1/conversations/{id}/messages', {
      path: { id: deal.conversation_id }, body: { text: pick(buyerLines) }, expect: [201], idempotent: true, read: true,
    }));
    if (sent) await inbox.delivered((f) => f.kind === 'message' && f.message_id === sent.id, since, messageDelivery);
    await pause(2, 6);

    await seller.send('GET', '/v1/conversations/{id}/messages', { path: { id: deal.conversation_id } });
    if (sent) await seller.send('POST', '/v1/conversations/{id}/read', { path: { id: deal.conversation_id }, body: { message_id: sent.id }, expect: [204] });
    await pause(3, 8);
    await seller.send('POST', '/v1/conversations/{id}/messages', {
      path: { id: deal.conversation_id }, body: { text: pick(sellerLines) }, expect: [201], idempotent: true,
    });
    await pause(2, 5);
    if (offered.status === 200) await seller.send('POST', '/v1/deals/{id}/decline', { path: { id: deal.id } });
  } finally {
    inbox.close();
  }
}
