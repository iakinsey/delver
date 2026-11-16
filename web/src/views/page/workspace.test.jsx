import React, { useState } from "react";
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { CrawlEditor } from "./workspace";
const definition = () => ({
  name: "Untitled crawl",
  seeds: [],
  feeds: [],
  scope: "same_domain",
  strategy: "host_diverse",
  max_depth: 10,
  max_hops: 3,
  allow_pattern: "",
  deny_pattern: "",
  request_delay: "100ms",
  concurrency: 2,
  revisit: "24h",
  proxy: { mode: "", url: "" },
  retention: { completed_for: "7d", failed_for: "30d", dedup_for: "90d" },
  termination: { when_drained: false, max_requests: 0, duration: "" },
});
function harness() {
  let latest;
  const save = vi.fn();
  function Form() {
    const [editor, setEditor] = useState({
      definition: definition(),
      record: null,
      pipeline: null,
    });
    latest = editor;
    return (
      <CrawlEditor
        editor={editor}
        setEditor={setEditor}
        busy={false}
        onSave={save}
        onCancel={() => {}}
        onError={() => {}}
      />
    );
  }
  render(<Form />);
  return { save, get: () => latest };
}
describe("guided crawl editor", () => {
  it("preserves multiline input and stable labels while updating intent", () => {
    const h = harness();
    const urls = "https://one.example/\nhttps://two.example/\n";
    fireEvent.change(screen.getByLabelText("Seed URLs"), {
      target: { value: urls },
    });
    expect(screen.getByLabelText("Seed URLs")).toHaveValue(urls);
    expect(h.get().definition.seeds).toEqual([
      "https://one.example/",
      "https://two.example/",
    ]);
    fireEvent.click(screen.getByRole("radio", { name: /Drain/ }));
    expect(h.get().definition.termination.when_drained).toBe(true);
    expect(screen.queryByLabelText("Revisit interval")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("radio", { name: /^Monitor/ }));
    expect(screen.getByLabelText("Revisit interval")).toHaveValue("24h");
  });
  it("configures a proxy and permanent deduplication without pipeline JSON", () => {
    const h = harness();
    fireEvent.click(screen.getByRole("radio", { name: /^SOCKS5$/ }));
    fireEvent.change(screen.getByLabelText("Proxy address"), {
      target: { value: "socks5://operator:secret@127.0.0.1:1080" },
    });
    fireEvent.click(
      screen.getByRole("checkbox", {
        name: "URL deduplication: no expiry",
      }),
    );
    expect(h.get().definition.proxy).toEqual({
      mode: "socks5",
      url: "socks5://operator:secret@127.0.0.1:1080",
    });
    expect(h.get().definition.retention.dedup_for).toBe("never");
    expect(screen.getByLabelText("URL deduplication")).toBeDisabled();
    expect(
      screen.queryByLabelText("Worker pipeline JSON"),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(h.save).toHaveBeenCalledWith(false);
  });
});
