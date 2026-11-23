// Load shapes. Each stage moves to level, a share of the peak that config.js
// sets, over minutes, which TIME_SCALE stretches.
export const profiles = {
  // A minute at the peak, to check the scripts and the stack.
  smoke: [
    { minutes: 0.25, level: 1 },
    { minutes: 1, level: 1 },
  ],
  // Up to the peak, twenty minutes there, down: does the stack hold it?
  load: [
    { minutes: 5, level: 1 },
    { minutes: 20, level: 1 },
    { minutes: 2, level: 0 },
  ],
  // Steps of half the peak up to three times it: where does it give?
  stress: [0.5, 1, 1.5, 2, 2.5, 3].flatMap((level) => [
    { minutes: 2, level },
    { minutes: 5, level },
  ]).concat([{ minutes: 2, level: 0 }]),
  // A jump to three times the peak, as a flash sale or a push to everyone.
  spike: [
    { minutes: 2, level: 0.2 },
    { minutes: 0.5, level: 3 },
    { minutes: 5, level: 3 },
    { minutes: 0.5, level: 0.2 },
    { minutes: 3, level: 0.2 },
  ],
  // Four hours at the peak: leaks, growing tables, connections left open.
  soak: [
    { minutes: 5, level: 1 },
    { minutes: 240, level: 1 },
    { minutes: 2, level: 0 },
  ],
};
