import { request } from "../../util";
export async function crawlAPI(action, data = {}) {
  const r = await request(`crawls/${action}`, data);
  if (!r.success) {
    const error = Error(r.error || "Could not complete this action");
    error.legacy = r.error === "Not found";
    throw error;
  }
  return r.data;
}
export const clone = (v) => JSON.parse(JSON.stringify(v));
export const strategyTitle = (v) =>
  ({
    host_diverse: "Balanced coverage",
    breadth_first: "Cover the surface",
    depth_first: "Follow a trail",
    adaptive: "Learn from results",
  })[v] || v;
export const operationTitle = (v) =>
  ({
    start_run: "Start run",
    continue_run: "Continue preserved work",
    activate: "Apply draft",
    start: "Start",
    pause: "Pause",
    resume: "Resume",
    stop: "Stop",
    replay: "Retry work",
    recrawl: "Revisit page",
  })[v] || words(v);
export const words = (v) => (v || "").replaceAll("_", " ");
