// Spotlight-style ranking: exact > prefix > word-start > substring > acronym
// > subsequence. Returns -1 for no match. Highlights are char indices.

export interface Match {
  score: number;
  indices: number[];
}

const isBoundary = (s: string, i: number) => i === 0 || /[\s\-_./:]/.test(s[i - 1]) || (/[a-z]/.test(s[i - 1]) && /[A-Z]/.test(s[i]));

export function fuzzyMatch(query: string, text: string): Match | null {
  const q = query.trim().toLowerCase();
  if (!q) return { score: 0, indices: [] };
  const t = text.toLowerCase();
  const range = (start: number, len: number) => Array.from({ length: len }, (_, k) => start + k);

  if (t === q) return { score: 1000, indices: range(0, q.length) };
  if (t.startsWith(q)) return { score: 900 - (t.length - q.length), indices: range(0, q.length) };

  // A word in the text starts with the query ("assist" → "Home Assistant").
  for (let i = 1; i < t.length; i++) {
    if (isBoundary(text, i) && t.startsWith(q, i)) {
      return { score: 750 - i, indices: range(i, q.length) };
    }
  }

  const sub = t.indexOf(q);
  if (sub >= 0) return { score: 500 - sub, indices: range(sub, q.length) };

  // Acronym: every query char matches a word start ("ha" → "Home Assistant").
  {
    const idx: number[] = [];
    let qi = 0;
    for (let i = 0; i < t.length && qi < q.length; i++) {
      if (isBoundary(text, i) && t[i] === q[qi]) {
        idx.push(i);
        qi++;
      }
    }
    if (qi === q.length && q.length > 1) return { score: 450, indices: idx };
  }

  // Subsequence with bonuses for consecutive runs and word starts.
  let score = 0;
  let qi = 0;
  let prev = -2;
  const idx: number[] = [];
  for (let i = 0; i < t.length && qi < q.length; i++) {
    if (t[i] === q[qi]) {
      score += 10;
      if (i === prev + 1) score += 15;
      if (isBoundary(text, i)) score += 20;
      idx.push(i);
      prev = i;
      qi++;
    }
  }
  if (qi < q.length) return null;
  const spread = idx[idx.length - 1] - idx[0];
  return { score: Math.max(1, Math.min(399, score - spread)), indices: idx };
}

/** Frecency boost from usage counts and recency, capped so text match dominates. */
export function frecencyBoost(count: number, lastUsedISO?: string): number {
  if (!count) return 0;
  let boost = Math.min(count, 20) * 4;
  if (lastUsedISO) {
    const ageH = (Date.now() - Date.parse(lastUsedISO)) / 36e5;
    if (ageH < 24) boost += 40;
    else if (ageH < 24 * 7) boost += 20;
  }
  return boost;
}
