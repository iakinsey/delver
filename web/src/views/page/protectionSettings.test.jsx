import React from "react";
import { describe, it, expect, vi, afterEach } from "vitest";
import {
  render,
  screen,
  fireEvent,
  waitFor,
  cleanup,
} from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { ProtectionSettings } from "./protectionSettings";
import { workspaceAPI } from "./crawlApi";
vi.mock("./crawlApi", async (original) => ({
  ...(await original()),
  workspaceAPI: vi.fn(),
}));
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});
const snapshot = {
  revision: 1,
  inspection_memory_mib: 256,
  lists: [],
  incidents: [],
  rules: [
    {
      id: "rule",
      name: "Rule",
      enabled: true,
      phrases: ["synthetic restricted marker"],
    },
  ],
};
it("disables configuration during saves and retains the completed server revision", async () => {
  let finish;
  workspaceAPI.mockImplementation((_route, { action }) =>
    action === "rules"
      ? new Promise((resolve) => {
          finish = resolve;
        })
      : Promise.resolve(structuredClone(snapshot)),
  );
  render(<ProtectionSettings section="restricted" onError={vi.fn()} />);
  const name = await screen.findByLabelText("Rule name 1");
  fireEvent.change(name, { target: { value: "Changed" } });
  fireEvent.click(screen.getByRole("button", { name: "Save rules" }));
  expect(name).toBeDisabled();
  expect(
    screen.getByLabelText("Inspection memory budget (MiB)"),
  ).toBeDisabled();
  finish({
    ...snapshot,
    revision: 2,
    rules: [{ ...snapshot.rules[0], name: "Changed" }],
  });
  await waitFor(() => expect(name).toBeEnabled());
  expect(name).toHaveValue("Changed");
  fireEvent.click(screen.getByRole("button", { name: "Save rules" }));
  expect(workspaceAPI).toHaveBeenLastCalledWith(
    "protection",
    expect.objectContaining({ action: "rules", revision: 2 }),
  );
  finish({
    ...snapshot,
    revision: 3,
    rules: [{ ...snapshot.rules[0], name: "Changed" }],
  });
  await waitFor(() => expect(name).toBeEnabled());
});
it("loads report pages separately and exports the full server record", async () => {
  const incident = {
    id: "incident",
    kind: "restricted",
    url: "http://synthetic.test/",
    rule_id: "rule",
    state: "completed",
    depth: 2,
    graph_revision: 4,
    partial: true,
    target_count: 201,
    targets: [],
    progress: { queue: "verified" },
  };
  workspaceAPI.mockImplementation((_route, { action, offset }) => {
    if (action === "incident")
      return Promise.resolve({
        ...incident,
        targets: [
          {
            site: offset ? "last.test" : "first.test",
            distance: offset ? 2 : 0,
          },
        ],
      });
    return Promise.resolve({ ...snapshot, incidents: [incident] });
  });
  render(<ProtectionSettings section="restricted" onError={vi.fn()} />);
  fireEvent.click(
    await screen.findByRole("button", {
      name: "http://synthetic.test/ · completed",
    }),
  );
  expect(await screen.findByText(/first.test/)).toBeVisible();
  expect(screen.getByRole("link", { name: "Export report" })).toHaveAttribute(
    "href",
    "/workspace/protection/report?id=incident",
  );
  fireEvent.click(screen.getByRole("button", { name: "Next sites" }));
  expect(await screen.findByText(/last.test/)).toBeVisible();
  expect(workspaceAPI).toHaveBeenCalledWith("protection", {
    action: "incident",
    id: "incident",
    offset: 200,
  });
  expect(screen.getByRole("button", { name: "Next sites" })).toBeDisabled();
});
