export const operationActive = (op) =>
  ["accepted", "running"].includes(op?.state);

const transitions = {
  start_run: "starting",
  continue_run: "starting",
  activate: "restarting",
  start: "resuming",
  pause: "pausing",
  resume: "resuming",
  stop: "stopping",
  delete: "deleting",
};

export function crawlState(record, operations = {}, pendingAction = "") {
  const transition = (action) =>
    action === "start_run" && record?.runs?.length
      ? "restarting"
      : transitions[action];
  if (transition(pendingAction)) return transition(pendingAction);
  const active = Object.values({ ...record?.operations, ...operations })
    .filter(operationActive)
    .sort((a, b) => (b.updated || 0) - (a.updated || 0))
    .find((op) => transitions[op.action]);
  if (active) return transition(active.action);
  const run = record?.runs?.at(-1);
  const runtime = record?.runtime?.state;
  if (
    [
      "starting",
      "pausing",
      "resuming",
      "restarting",
      "stopping",
      "deleting",
    ].includes(runtime)
  )
    return runtime;
  if (run?.state === "completed") return "completed";
  if (run?.state === "draining" && runtime === "running") return "draining";
  return runtime || "not started";
}
