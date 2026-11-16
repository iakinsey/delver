// Targeted interactions: no Cartesian product is generated or executed.
const base = Object.freeze({
  mode: "fixed",
  strategy: "host_diverse",
  source: "urls",
  scope: "same_origin",
  connection: "direct",
  requireProxy: false,
  isolateSessions: false,
  robots: true,
  restrictHosts: false,
  drain: false,
  retention: "finite",
  requestLimit: false,
  timeLimit: false,
});
const cases = [];
for (const mode of ["fixed", "adaptive"])
  for (const strategy of [
    "host_diverse",
    "breadth_first",
    "depth_first",
    "adaptive",
  ])
    cases.push({
      ...base,
      id: `traversal-${mode}-${strategy}`,
      mode,
      strategy,
    });
cases.push(
  {
    ...base,
    id: "feed-proxy-restriction",
    mode: "adaptive",
    source: "feeds",
    scope: "same_domain",
    connection: "socks5",
    requireProxy: true,
    restrictHosts: true,
    robots: false,
    drain: true,
    retention: "dedup",
    requestLimit: true,
  },
  {
    ...base,
    id: "mixed-sources-isolated-proxy",
    mode: "adaptive",
    source: "both",
    scope: "open_web",
    connection: "socks5",
    requireProxy: true,
    isolateSessions: true,
    restrictHosts: true,
    retention: "permanent",
    timeLimit: true,
  },
  {
    ...base,
    id: "http-drain-permanent-details",
    source: "both",
    scope: "open_web",
    connection: "http",
    requireProxy: true,
    robots: false,
    drain: true,
    retention: "completed",
    requestLimit: true,
  },
  {
    ...base,
    id: "feed-monitor-failed-retention",
    mode: "adaptive",
    source: "feeds",
    connection: "http",
    retention: "failed",
    requestLimit: true,
    timeLimit: true,
  },
);
for (const [requestLimit, timeLimit] of [
  [false, false],
  [true, false],
  [false, true],
  [true, true],
])
  cases.push({
    ...base,
    id: `limits-${requestLimit}-${timeLimit}`,
    mode: "adaptive",
    scope: "same_domain",
    connection: "socks5",
    requireProxy: true,
    requestLimit,
    timeLimit,
  });
export const interactionScenarios = Object.freeze(cases);
export function* configurations() {
  yield* interactionScenarios;
}
export const scenarioId = (v) => v.id;
export const expectedScenarioCount = interactionScenarios.length;
