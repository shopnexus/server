import { Api } from '../lib/api.js';
import { chance, think } from '../lib/think.js';
import { openListing, search } from './browse.js';

// A guest's visit, on the website or the app before sign-in: home, a search,
// a listing and sometimes its seller.
export function guestVisit(catalog) {
  const api = new Api('guest');
  api.get('/v1/screens/home');
  think(2, 6);
  const found = search(api, catalog);
  think(3, 8);
  const listing = openListing(api, catalog, found);
  if (chance(0.3)) {
    think(3, 8);
    api.get('/v1/screens/seller/{id}', { path: { id: listing.seller } });
  }
}
