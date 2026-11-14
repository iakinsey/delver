import { RPC_HOST, RPC_PORT } from "../config";
export default class Connection {
  constructor(filters, onMessage, onDisconnect) {
    this.filters = filters;
    this.onMessage = onMessage;
    this.onDisconnect = onDisconnect;
    this.connected = false;
    this.intentionalClose = false;
  }
  disconnect() {
    this.intentionalClose = true;
    this.ws?.close();
  }
  close() {
    this.disconnect();
  }
  connect() {
    const protocol = location.protocol === "https:" ? "wss" : "ws";
    this.ws = new WebSocket(`${protocol}://${RPC_HOST}:${RPC_PORT}/`);
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        this.ws.close();
        reject(new Error("Authentication timed out"));
      }, 10000);
      this.ws.onopen = () =>
        this.ws.send(
          JSON.stringify({
            version: 1,
            type: "authenticate",
            token: localStorage.auth,
          }),
        );
      this.ws.onmessage = (event) => {
        let msg;
        try {
          msg = JSON.parse(event.data);
        } catch {
          return;
        }
        if (msg.type === "authenticated") {
          clearTimeout(timeout);
          this.connected = true;
          resolve(this.ws);
        } else if (msg.type === "auth_error") {
          clearTimeout(timeout);
          this.intentionalClose = true;
          reject(new Error("Session expired"));
          window.dispatchEvent(new Event("session-expired"));
          this.ws.close();
        } else this.onMessage(msg);
      };
      this.ws.onerror = () => {
        clearTimeout(timeout);
        reject(new Error("Connection failed"));
      };
      this.ws.onclose = () => {
        clearTimeout(timeout);
        const opened = this.connected;
        this.connected = false;
        if (!opened)
          reject(new Error("Connection closed before authentication"));
        if (!this.intentionalClose) this.onDisconnect?.();
      };
    });
  }
  send(message) {
    if (this.connected)
      this.ws.send(JSON.stringify({ version: 1, ...message }));
  }
}
