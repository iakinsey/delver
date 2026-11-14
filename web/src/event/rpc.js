import Connection from "./conn";
export default class RPC {
  constructor() {
    this.filters = {};
    this.callbacks = {};
    this.statuses = {};
    this.conn = null;
    this.retryTimer = null;
    this.attempt = 0;
  }
  addFilter(id, filter, callback, status = () => {}) {
    this.filters[id] = JSON.parse(filter);
    this.callbacks[id] = callback;
    this.statuses[id] = status;
    if (this.conn?.connected)
      this.conn.send({ type: "subscribe", id, filter: this.filters[id] });
    else this.ensureConnection();
  }
  removeFilter(id) {
    if (!this.filters[id]) return;
    delete this.filters[id];
    delete this.callbacks[id];
    delete this.statuses[id];
    this.conn?.send({ type: "remove", id });
    if (!Object.keys(this.filters).length) {
      clearTimeout(this.retryTimer);
      this.conn?.disconnect();
      this.conn = null;
    }
  }
  getFilters() {
    return JSON.stringify(Object.values(this.filters));
  }
  async ensureConnection() {
    if (
      this.connecting ||
      this.conn?.connected ||
      !Object.keys(this.filters).length
    )
      return;
    clearTimeout(this.retryTimer);
    this.connecting = true;
    Object.values(this.statuses).forEach((fn) =>
      fn({ connection: "Connecting…" }),
    );
    const connection = new Connection(
      "",
      (msg) => {
        if (this.conn === connection) this.onMessage(msg);
      },
      () => {
        if (this.conn === connection) this.onDisconnect();
      },
    );
    this.conn = connection;
    try {
      await connection.connect();
      if (this.conn !== connection) return;
      this.attempt = 0;
      Object.entries(this.filters).forEach(([id, filter]) =>
        connection.send({ type: "subscribe", id, filter }),
      );
      Object.values(this.statuses).forEach((fn) =>
        fn({ connection: "Connected", loading: true }),
      );
    } catch {
      this.scheduleRetry();
    } finally {
      this.connecting = false;
      if (!this.conn && Object.keys(this.filters).length)
        this.ensureConnection();
    }
  }
  async reconnect() {
    this.conn?.disconnect();
    this.conn = null;
    return this.ensureConnection();
  }
  scheduleRetry() {
    clearTimeout(this.retryTimer);
    if (!Object.keys(this.filters).length) return;
    Object.values(this.statuses).forEach((fn) =>
      fn({ connection: "Disconnected — retrying", loading: false }),
    );
    this.retryTimer = setTimeout(
      () => {
        this.conn = null;
        this.ensureConnection();
      },
      Math.min(30000, 1000 * 2 ** this.attempt++),
    );
  }
  onDisconnect() {
    this.scheduleRetry();
  }
  onMessage(msg) {
    const id = msg.data?.[0]?.callback;
    if (!id || !this.callbacks[id]) return;
    const status = this.statuses[id];
    if (msg.type === "error")
      status?.({ error: msg.data[0].message, loading: false });
    else if (msg.type === "history_limit")
      status?.({ warning: msg.data[0].message });
    else if (msg.type === "preload_complete") status?.({ loading: false });
    else if (msg.type === "subscribed")
      status?.({ loading: true, error: "", warning: "" });
    else if (msg.type !== "removed") {
      this.callbacks[id](msg);
      status?.({ lastUpdate: Date.now(), loading: false, hasData: true });
    }
  }
}
