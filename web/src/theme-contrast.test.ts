import { describe, expect, it } from "vitest";
import css from "./index.css?raw";

// The theme's colours are checked by calculation, not by eye: each text colour must keep WCAG
// AA contrast (4.5:1 for normal text) on the surface it sits on (spec 7.7).

type RGB = [number, number, number];

// tokens reads the oklch tokens of one block of index.css (":root" or ".dark").
function tokens(block: string): Record<string, { rgb: RGB; alpha: number; l: number }> {
  const start = css.indexOf(`${block} {`);
  const body = css.slice(start, css.indexOf("}", start));
  const out: Record<string, { rgb: RGB; alpha: number; l: number }> = {};
  for (const m of body.matchAll(/--([\w-]+):\s*oklch\(([\d.]+) ([\d.]+) ([\d.]+)(?: \/ ([\d.]+)%)?\)/g)) {
    const [l, c, h] = [Number(m[2]), Number(m[3]), Number(m[4])];
    out[m[1]] = { rgb: oklchToSrgb(l, c, h), alpha: m[5] === undefined ? 1 : Number(m[5]) / 100, l };
  }
  return out;
}

// oklchToSrgb converts to gamma-encoded sRGB in 0..1, clipped to the gamut.
function oklchToSrgb(l: number, c: number, h: number): RGB {
  const a = c * Math.cos((h * Math.PI) / 180);
  const b = c * Math.sin((h * Math.PI) / 180);
  const L = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const M = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const S = (l - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const lin: RGB = [
    4.0767416621 * L - 3.3077115913 * M + 0.2309699292 * S,
    -1.2684380046 * L + 2.6097574011 * M - 0.3413193965 * S,
    -0.0041960863 * L - 0.7034186147 * M + 1.707614701 * S,
  ];
  return lin.map((x) => {
    const v = Math.min(Math.max(x, 0), 1);
    return v <= 0.0031308 ? 12.92 * v : 1.055 * v ** (1 / 2.4) - 0.055;
  }) as RGB;
}

// over composites a colour with an alpha over another, in gamma sRGB as browsers do.
const over = (fg: RGB, alpha: number, bg: RGB): RGB => fg.map((f, i) => alpha * f + (1 - alpha) * bg[i]) as RGB;

function luminance([r, g, b]: RGB): number {
  const lin = (v: number) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

function contrast(a: RGB, b: RGB): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

// The scan details sit on bg-muted/50 over the header's background.
const DETAILS_ALPHA = 0.5;
// The out-of-date notice and the attention mark: amber-700 in light, amber-400 in dark.
const AMBER = { light: oklchToSrgb(0.555, 0.163, 48.998), dark: oklchToSrgb(0.828, 0.189, 84.429) };

describe("the dark theme", () => {
  const t = tokens(".dark");
  const surface = over(t.muted.rgb, DETAILS_ALPHA, t.background.rgb);

  it("is a soft dark gray-blue with off-white text, not near-black and pure white", () => {
    expect(t.background.l).toBeGreaterThanOrEqual(0.2);
    expect(t.background.l).toBeLessThanOrEqual(0.3);
    expect(t.foreground.l).toBeLessThan(0.97);
    expect(t.card.l).toBeGreaterThan(t.background.l);
  });

  it.each([
    ["foreground", "background"],
    ["muted-foreground", "background"],
    ["muted-foreground", "muted"],
    ["muted-foreground", "card"],
    ["primary-foreground", "primary"],
    ["destructive", "background"],
    ["card-foreground", "card"],
    ["popover-foreground", "popover"],
    ["secondary-foreground", "secondary"],
    ["accent-foreground", "accent"],
  ])("keeps %s on %s at 4.5:1 or more", (fg, bg) => {
    expect(contrast(t[fg].rgb, t[bg].rgb)).toBeGreaterThanOrEqual(4.5);
  });

  it("keeps the text of the scan details at 4.5:1 or more on their surface", () => {
    for (const fg of [t.foreground.rgb, t["muted-foreground"].rgb, t.destructive.rgb, AMBER.dark]) {
      expect(contrast(fg, surface)).toBeGreaterThanOrEqual(4.5);
    }
  });
});

describe("the light theme", () => {
  const t = tokens(":root");
  const surface = over(t.muted.rgb, DETAILS_ALPHA, t.background.rgb);

  it("keeps the text of the scan details at 4.5:1 or more on their surface", () => {
    for (const fg of [t.foreground.rgb, t["muted-foreground"].rgb, t.destructive.rgb, AMBER.light]) {
      expect(contrast(fg, surface)).toBeGreaterThanOrEqual(4.5);
    }
  });
});
