import React from "react";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { MemoryRouter, Route } from "react-router-dom";
import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import CrawlReport from "./crawlReport";
import { apiRequest } from "./crawlApi";
vi.mock("./crawlApi", () => ({ apiRequest: vi.fn() }));
vi.mock("./hostGraph", () => ({
  HostGraph: ({ view }) => (
    <div data-testid="graph">{view.nodes.length} graph nodes</div>
  ),
}));
const summary = {
  run_id: "run-1",
  state: "completed",
  requests_per_second: 12.25,
  rate_basis: "average",
  counts: { known: 8, visited: 6, not_visited: 2, partial: false },
  backfill_pending: false,
};
function mount() {
  return render(
    <MemoryRouter initialEntries={["/crawls/crawl-1/report?run_id=run-1"]}>
      <Route path="/crawls/:crawlId/report">
        <CrawlReport />
      </Route>
    </MemoryRouter>,
  );
}
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
beforeEach(() => {
  apiRequest.mockImplementation(async (path) => {
    if (path.endsWith("/report")) return summary;
    if (path.endsWith("/hosts"))
      return [{ id: 1, name: "example.test", visited: 6, known: 8 }];
    if (path.endsWith("/host_graph"))
      return {
        nodes: [
          {
            id: "h:1",
            name: "example.test",
            selected: true,
            visited: 6,
            known: 8,
            authority: 0.5,
            percentile: 90,
            incoming: 2,
            outgoing: 4,
            bridges: 1,
          },
        ],
        edges: [],
        total_hosts: 1,
        total_edges: 0,
        revision: 1,
        analyzed_at: new Date().toISOString(),
      };
    throw Error(path);
  });
});
describe("crawl report", () => {
  it("shows agreed metrics and historical rate basis without opening the graph", async () => {
    mount();
    await screen.findByText("12.25");
    expect(screen.getByText("URLs visited")).toBeVisible();
    expect(screen.getByText("URLs not visited")).toBeVisible();
    expect(screen.getByText("Average over running time")).toBeVisible();
    expect(screen.queryByTestId("graph")).not.toBeInTheDocument();
    expect(
      apiRequest.mock.calls.some(([path]) => path.endsWith("host_graph")),
    ).toBe(false);
  });
  it("opens a selected host with default depth and exposes authority and bridge facts", async () => {
    mount();
    await screen.findByText("Requests/s");
    const search = screen.getByRole("combobox");
    fireEvent.focus(search);
    fireEvent.change(search, { target: { value: "example" } });
    await screen.findByRole("option");
    fireEvent.keyDown(search, { key: "ArrowDown" });
    fireEvent.keyDown(search, { key: "Enter" });
    await screen.findByTestId("graph");
    expect(screen.getByText(/Authority percentile: 90/)).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Grouped", exact: true }),
    ).toBeDisabled();
    const call = apiRequest.mock.calls.find(([path]) =>
      path.endsWith("host_graph"),
    );
    expect(call[1]).toMatchObject({
      host: "example.test",
      depth: 2,
      mode: "hosts",
      run_id: "run-1",
    });
  });
  it("uses ranked cursors when loading more host suggestions", async () => {
    const rows = Array.from({ length: 21 }, (_, i) => ({
      id: i + 1,
      name: `h${String(i).padStart(2, "0")}.test`,
      known: 1,
      visited: 0,
      connections: 21 - i,
      cursor: `connections:${21 - i}:h${String(i).padStart(2, "0")}.test`,
    }));
    apiRequest.mockImplementation(async (path, data) => {
      if (path.endsWith("/report")) return summary;
      if (path.endsWith("/hosts"))
        return data.after ? rows.slice(20) : rows.slice(0, 20);
      throw Error(path);
    });
    mount();
    await screen.findByText("Requests/s");
    fireEvent.focus(screen.getByRole("combobox"));
    await waitFor(() => expect(screen.getAllByRole("option")).toHaveLength(20));
    expect(screen.getAllByRole("option")[0]).toHaveTextContent("h00.test");
    expect(screen.getAllByRole("option")[0]).toHaveTextContent(
      "21 connections",
    );
    fireEvent.click(screen.getByRole("button", { name: "More hosts" }));
    await waitFor(() => expect(screen.getAllByRole("option")).toHaveLength(21));
    expect(
      apiRequest.mock.calls.find(
        ([path, data]) => path.endsWith("/hosts") && data.after,
      )?.[1].after,
    ).toBe(rows[19].cursor);
  });
  it("labels partial historical coverage and indexing errors", async () => {
    apiRequest.mockResolvedValue({
      ...summary,
      counts: { ...summary.counts, partial: true },
      backfill_error: "Indexed history unavailable",
    });
    mount();
    await screen.findByText("Partial history");
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Indexed history unavailable",
    );
  });
  it("reports failures without manufacturing zero counts", async () => {
    apiRequest.mockRejectedValue(Error("run required"));
    mount();
    await screen.findByRole("alert");
    expect(screen.getByRole("alert")).toHaveTextContent("run required");
    expect(
      screen.queryByLabelText("Run report metrics"),
    ).not.toBeInTheDocument();
  });
});
