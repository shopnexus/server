import { Api } from '../lib/api.js';
import { vuSession } from '../lib/session.js';
import { chance, think } from '../lib/think.js';
import { openListing, search } from './browse.js';

// A signed-in visit: home and unread counts, a search, a listing it marks as
// viewed, and now and then a save, the inbox or the saved list. A save is
// undone in the same visit, so runs leave the accounts as they found them.
export function memberVisit(catalog) {
  const api = new Api('member', vuSession());
  api.get('/v1/screens/home');
  api.get('/v1/me/unread');
  think(2, 6);
  const found = search(api, catalog);
  think(3, 8);
  const listing = openListing(api, catalog, found);
  api.put('/v1/viewed/{listing_id}', { path: { listing_id: listing.id } });
  if (chance(0.2)) {
    think(2, 4);
    api.put('/v1/saved/{listing_id}', { path: { listing_id: listing.id } });
    think(1, 3);
    api.del('/v1/saved/{listing_id}', { path: { listing_id: listing.id } });
  }
  if (chance(0.3)) {
    think(3, 8);
    api.get('/v1/screens/inbox');
  }
  if (chance(0.15)) {
    think(2, 5);
    api.get('/v1/saved');
  }
}
