/** Space kept between the scrolled-in link and the row edge, so the fade never covers it. */
const EDGE_GAP = 24;

/**
 * Scrolls `link` fully into view inside the horizontal `row` only, so the
 * page itself never scrolls (FR-005 AC7). Works in LTR and RTL because it
 * adjusts `scrollLeft` by on-screen deltas.
 */
export function revealInRow(row: HTMLElement, link: HTMLElement): void {
  if (row.scrollWidth <= row.clientWidth) return;
  const r = row.getBoundingClientRect();
  const l = link.getBoundingClientRect();
  if (l.left < r.left + EDGE_GAP) {
    row.scrollLeft -= r.left + EDGE_GAP - l.left;
  } else if (l.right > r.right - EDGE_GAP) {
    row.scrollLeft += l.right - (r.right - EDGE_GAP);
  }
}
