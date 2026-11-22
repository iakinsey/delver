import { describe, it, expect } from "vitest";
import { tileRequests, tileGeometry, selectDrawTiles } from "./networkTiles";
describe("fixed network tile geometry", () => {
  it("keeps the same world point at the same screen position at every tile level", () => {
    const point = { x: 0.4123, y: 0.3876 };
    for (const zoom of [0.5, 1, 2, 10, 100, 10000])
      for (const [width, height] of [
        [390, 844],
        [1280, 900],
      ]) {
        const camera = { x: 0.4, y: 0.38, zoom },
          unit = Math.min(width, height) * zoom;
        for (let z = 0; z <= 16; z++) {
          const grid = 2 ** z,
            x = Math.floor(point.x * grid),
            y = Math.floor(point.y * grid);
          const tile = { z, x, y },
            rect = tileGeometry(tile, camera, width, height);
          expect(rect.x + (point.x * grid - x) * rect.size).toBeCloseTo(
            (point.x - camera.x) * unit + width / 2,
            6,
          );
          expect(rect.y + (point.y * grid - y) * rect.size).toBeCloseTo(
            (point.y - camera.y) * unit + height / 2,
            6,
          );
        }
      }
  });
  it("bounds decoded tile requests, including large layouts and rapid zoom extremes", () => {
    for (const zoom of [0.5, 1, 2, 100, 10000])
      for (const x of [-0.5, 0.5, 1.5])
        for (const [width, height] of [
          [390, 844],
          [1280, 1200],
          [4096, 4096],
        ]) {
          const tiles = tileRequests({ x, y: 0.5, zoom }, width, height);
          expect(tiles.length).toBeLessThanOrEqual(37);
          expect(tiles[0]).toEqual({ z: 0, x: 0, y: 0 });
          for (const t of tiles) {
            expect(t.x).toBeGreaterThanOrEqual(0);
            expect(t.y).toBeGreaterThanOrEqual(0);
            expect(t.x).toBeLessThan(2 ** t.z);
            expect(t.y).toBeLessThan(2 ** t.z);
          }
        }
  });
});

it("does not overlay cached close-up tiles after zooming back to the overview", () => {
  const values = [0, 1, 7, 12].map((z) => ({
    z,
    x: 0,
    y: 0,
    host: 0,
    snapshot: "same",
  }));
  values.push(
    { z: 1, x: 0, y: 0, host: 7, snapshot: "same" },
    { z: 0, x: 0, y: 0, host: 0, snapshot: "old" },
  );
  const overview = selectDrawTiles(
    values,
    "same",
    0,
    { x: 0.5, y: 0.5, zoom: 1 },
    800,
    600,
  );
  expect(overview.map((t) => t.z)).toEqual([0, 1]);
  const closer = selectDrawTiles(
    values,
    "same",
    0,
    { x: 0.5, y: 0.5, zoom: 200 },
    800,
    600,
  );
  expect(closer.map((t) => t.z)).toEqual([0, 1, 7]);
});
