import { request } from "../../util";
export const crawlAPI = (action, data = {}) =>
  apiRequest(`crawls/${action}`, data);
export const workspaceAPI = (action, data = {}) =>
  apiRequest(`workspace/${action}`, data);
export async function apiRequest(path, data = {}) {
  const r = await request(path, data);
  if (!r.success) {
    const error = Error(r.error || "Request failed");
    throw error;
  }
  return r.data;
}
export const clone = (v) => JSON.parse(JSON.stringify(v));
export const strategyTitle = (v) =>
  ({
    host_diverse: "Host-diverse",
    breadth_first: "Breadth-first",
    depth_first: "Depth-first",
    adaptive: "Adaptive",
  })[v] || v;
export const operationTitle = (v) =>
  ({
    start_run: "Start",
    continue_run: "Drain",
    activate: "Apply draft",
    start: "Start",
    pause: "Pause",
    resume: "Resume",
    stop: "Stop",
    replay: "Retry",
    recrawl: "Recrawl",
  })[v] || words(v);
export const words = (v) => (v || "").replaceAll("_", " ");
