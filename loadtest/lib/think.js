import { sleep } from 'k6';
import { config } from './config.js';

// think pauses like a person reading a screen, between min and max seconds.
export function think(min, max) {
  if (config.thinkScale > 0) sleep((min + Math.random() * (max - min)) * config.thinkScale);
}

// chance is true with probability p.
export function chance(p) {
  return Math.random() < p;
}

// pick returns a random element of list, or undefined when it is empty.
export function pick(list) {
  return list.length ? list[Math.floor(Math.random() * list.length)] : undefined;
}
