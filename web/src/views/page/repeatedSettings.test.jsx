import React, { useState } from "react";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import {
  RepeatedSettings,
  repeatedDefaults,
  repeatedFields,
  ExpertRepeatedSettings,
} from "./repeatedSettings";
import { crawlAPI } from "./crawlApi";
vi.mock("./crawlApi", () => ({ crawlAPI: vi.fn() }));
function Harness({ custom = false }) {
  const [value, setValue] = useState();
  return <RepeatedSettings custom={custom} value={value} onChange={setValue} />;
}
describe("repeated settings", () => {
  it("applies guided defaults and preserves fields through mode changes", () => {
    render(<Harness />);
    const mode = screen.getByLabelText("Repeated-page mode");
    expect(mode).toHaveValue("deprioritize");
    for (const [label, key] of repeatedFields)
      expect(screen.getByLabelText(label)).toHaveValue(repeatedDefaults[key]);
    fireEvent.change(screen.getByLabelText("Repeated admission share (%)"), {
      target: { value: "25" },
    });
    fireEvent.change(mode, { target: { value: "off" } });
    expect(screen.queryByLabelText("Repeated admission share (%)")).toBeNull();
    fireEvent.change(mode, { target: { value: "observe" } });
    expect(screen.getByLabelText("Repeated admission share (%)")).toHaveValue(
      25,
    );
  });
  it("keeps absent custom configuration off", () => {
    render(<Harness custom />);
    expect(screen.getByLabelText("Repeated-page mode")).toHaveValue("off");
  });
  it("patches the original expert JSON rather than serializing rounded integers", async () => {
    const pipeline =
      '{"resources":[{"name":"frontier","type":"frontier_store","parameters":{"opaque":9007199254740993123}}]}';
    const onApply = vi.fn();
    crawlAPI.mockResolvedValue({ configuration_json: "exact result" });
    render(
      <ExpertRepeatedSettings
        pipeline={pipeline}
        onApply={onApply}
        onError={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByLabelText("Repeated-page mode"), {
      target: { value: "observe" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Apply repeated-page settings" }),
    );
    await waitFor(() => expect(onApply).toHaveBeenCalledWith("exact result"));
    expect(crawlAPI).toHaveBeenCalledWith("patch_repeated_pages", {
      configuration_json: pipeline,
      settings: { frontier: { ...repeatedDefaults, mode: "observe" } },
    });
  });
});
