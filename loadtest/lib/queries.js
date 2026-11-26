import { chance, pick } from './think.js';

// What people add to a search term, and places the filters use. Codes are
// the seeded provinces and communes.
const modifiers = ['cũ', 'giá rẻ', 'chính hãng', 'like new', 'hà nội', 'sài gòn', '2023', 'pro', 'mini', 'đã qua sử dụng'];
export const provinces = ['01', '79', '48', '31', '92'];
export const communes = ['00004', '25747'];
export const priceRanges = [[0, 1_000_000], [1_000_000, 5_000_000], [5_000_000, 20_000_000], [20_000_000, 0]];
export const conditions = ['new', 'like_new', 'used'];

// fold drops the diacritics, as people type on a keyboard without them.
function fold(text) {
  return text.normalize('NFD').replace(/[̀-ͯ]/g, '').replace(/đ/g, 'd').replace(/Đ/g, 'D');
}

// queryText is a head term half the time, otherwise one with a word added
// or typed without diacritics: a long tail, as real searches have, instead
// of the same twenty terms over and over.
export function queryText(terms) {
  let q = pick(terms);
  if (chance(0.5)) return q;
  if (chance(0.6)) q = `${q} ${pick(modifiers)}`;
  return chance(0.4) ? fold(q) : q;
}
