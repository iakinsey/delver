import React, { useState } from "react";
import { it, expect, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import { TrapSettings, trapDefaults } from "./trapSettings";
afterEach(cleanup);
it("retains configured thresholds when detection is toggled", () => {
  function Harness() {
    const [value, set] = useState({
      ...trapDefaults,
      similarity: 0.85,
      cross_site_links: 0.65,
    });
    return <TrapSettings value={value} onChange={set} />;
  }
  render(<Harness />);
  fireEvent.click(screen.getByText("Trap detection"));
  expect(screen.getByLabelText("Structure similarity")).toHaveValue(0.85);
  fireEvent.click(screen.getByLabelText("Detect crawler traps"));
  expect(screen.queryByLabelText("Structure similarity")).toBeNull();
  fireEvent.click(screen.getByLabelText("Detect crawler traps"));
  expect(screen.getByLabelText("Structure similarity")).toHaveValue(0.85);
  expect(screen.getByLabelText("Minimum cross-site link fraction")).toHaveValue(
    0.65,
  );
});
