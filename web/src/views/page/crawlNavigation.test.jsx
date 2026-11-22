import React from "react";
import { describe, it, expect } from "vitest";
import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import { Router } from "react-router-dom";
import { createMemoryHistory } from "history";
import CrawlNavigation from "./crawlNavigation";

describe("Crawl navigation", () => {
  for (const active of ["Crawl", "Reports", "Graph"])
    it(`shares navigation from ${active} and preserves historical reports`, () => {
      cleanup();
      const history = createMemoryHistory({
        initialEntries: ["/crawls/crawl-id/network?run_id=run-id"],
      });
      render(
        <Router history={history}>
          <CrawlNavigation crawlId="crawl-id" active={active} />
        </Router>,
      );
      expect(screen.getAllByRole("button").map((b) => b.textContent)).toEqual([
        "Crawl",
        "Reports",
        "Graph",
      ]);
      expect(
        screen
          .getByRole("button", { name: active })
          .getAttribute("aria-current"),
      ).toBe("page");
      fireEvent.click(screen.getByRole("button", { name: "Reports" }));
      expect(history.location.pathname + history.location.search).toBe(
        "/crawls/crawl-id/report?run_id=run-id",
      );
      fireEvent.click(screen.getByRole("button", { name: "Graph" }));
      expect(history.location.pathname + history.location.search).toBe(
        "/crawls/crawl-id/network?run_id=run-id",
      );
      fireEvent.click(screen.getByRole("button", { name: "Crawl" }));
      expect(history.location.pathname + history.location.search).toBe(
        "/crawls/crawl-id",
      );
    });
});
