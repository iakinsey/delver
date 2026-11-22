import { describe, it, expect } from "vitest";
import { graphBounds, nodeRadius, clampCamera } from "./networkGraph";
describe("network rendering bounds", () => {
  it("bounds every pan/zoom update including invalid geometry", () => {
    expect(clampCamera({ x: 100, y: -100, zoom: 1e9 })).toEqual({
      x: 1.5,
      y: -0.5,
      zoom: 10000,
    });
    expect(clampCamera({ x: NaN, y: Infinity, zoom: 0 })).toEqual({
      x: 0.5,
      y: 0.5,
      zoom: 0.5,
    });
  });
  it("bounds the world viewport and keeps aspect ratio", () => {
    expect(graphBounds({ x: 0.5, y: 0.5, zoom: 1 }, 800, 800)).toEqual([
      0, 0, 1, 1,
    ]);
    expect(graphBounds({ x: 0.5, y: 0.5, zoom: 2 }, 800, 400)).toEqual([
      0, 0.25, 1, 0.75,
    ]);
    expect(graphBounds({ x: 100, y: -100, zoom: 1 }, 800, 800)).toEqual([
      2, -1, 2, -1,
    ]);
  });
  it("strictly caps host and aggregate sizes", () => {
    for (const authority of [0, 1e-8, 0.0001, 1, 1000000])
      for (const hosts of [1, 100000, 2000000]) {
        const radius = nodeRadius({ authority }, hosts);
        expect(radius).toBeGreaterThanOrEqual(1.4);
        expect(radius).toBeLessThanOrEqual(8);
      }
  });
});
