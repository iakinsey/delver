import React, { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { FetchSettings, adaptiveOption } from "./fetchSettings";
import options from "./fetchOptions.json";
afterEach(cleanup);
for (const mode of ["fixed", "adaptive"]) {
  describe(mode, () => {
    for (const option of options) {
      it(`${option.key} exposes both boundaries and preserves sibling settings`, () => {
        const value = {
          mode,
          ...Object.fromEntries(
            options.map((o) => [o.key, o.kind.includes("duration") ? "1s" : 1]),
          ),
        };
        const change = vi.fn();
        function Form() {
          const [draft, setDraft] = useState(value);
          return (
            <FetchSettings
              value={draft}
              onChange={(next) => {
                change(next);
                setDraft(next);
              }}
              onError={vi.fn()}
            />
          );
        }
        render(<Form />);
        if (mode === "fixed" && adaptiveOption(option.key)) {
          expect(screen.queryByLabelText(option.label)).not.toBeInTheDocument();
          return;
        }
        const field = screen.getByLabelText(option.label);
        if (!option.kind.includes("duration")) {
          expect(Number(field.min)).toBe(option.min);
          expect(Number(field.max)).toBe(option.max);
        }
        for (const boundary of [option.min, option.max]) {
          const input = option.kind.includes("duration")
            ? `${boundary}s`
            : String(boundary);
          fireEvent.change(field, { target: { value: "" } });
          fireEvent.change(field, { target: { value: input } });
          expect(change).toHaveBeenLastCalledWith({
            ...value,
            [option.key]: option.kind.includes("duration") ? input : boundary,
          });
        }
        fireEvent.change(field, { target: { value: "" } });
        expect(change).toHaveBeenLastCalledWith({ ...value, [option.key]: "" });
        if (option.kind === "inherit_duration") {
          fireEvent.change(field, { target: { value: "inherit" } });
          expect(change).toHaveBeenLastCalledWith({
            ...value,
            [option.key]: "inherit",
          });
        }
      });
    }
  });
}
