import React from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { MemoryRouter, Route } from "react-router-dom";
import { afterEach, describe, it, expect, vi } from "vitest";
import NetworkGraph, {
  graphBounds,
  nodeRadius,
  clampCamera,
} from "./networkGraph";
import { apiRequest } from "./crawlApi";

vi.mock("./crawlApi", () => ({ apiRequest: vi.fn() }));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
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

it("groups the trap filter with the other generation filters and switches snapshot requests", async () => {
  apiRequest.mockResolvedValue({ snapshot: null, job: null });
  render(
    <MemoryRouter initialEntries={["/crawls/crawl-1/network?run_id=run-1"]}>
      <Route path="/crawls/:crawlId/network">
        <NetworkGraph />
      </Route>
    </MemoryRouter>,
  );
  fireEvent.click(screen.getByText("Generation parameters"));
  const filter = screen.getByRole("checkbox", {
    name: "Hide trap-excluded hosts",
  });
  expect(filter).toBeChecked();
  const controls = screen.getByRole("group", { name: "Host filters" });
  expect(controls).toContainElement(filter);
  expect(controls).toContainElement(
    screen.getByRole("checkbox", { name: "Exclude orphans" }),
  );
  expect(controls).not.toContainElement(
    screen.getByRole("spinbutton", { name: "Host limit" }),
  );
  expect(controls).toContainElement(
    screen.getByRole("checkbox", { name: "Visited hosts only" }),
  );
  await waitFor(() =>
    expect(apiRequest).toHaveBeenCalledWith(
      "crawls/atlas_status",
      expect.objectContaining({ run_id: "run-1", hide_traps: true }),
      expect.anything(),
    ),
  );
  fireEvent.click(filter);
  await waitFor(() =>
    expect(apiRequest).toHaveBeenCalledWith(
      "crawls/atlas_status",
      expect.objectContaining({ run_id: "run-1", hide_traps: false }),
      expect.anything(),
    ),
  );
});
