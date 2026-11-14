import React from "react";
import RPC from "../event/rpc";
import { uuid4 } from "../util";
import store from "../event/store";
import { updateCell, deleteCell } from "../event/actions";
import { rememberRemoved } from "../event/dirty";
import ClickableText from "./clickableText";
const rpc = new RPC();
export default class Connectable extends React.Component {
  constructor(props) {
    super(props);
    this.uuid = props.cell.uuid || uuid4();
  }
  componentDidMount() {
    if (this.props.cell.filter) this.connect();
  }
  componentDidUpdate(previous) {
    this.cell = this.props.cell;
    if (
      JSON.stringify(previous.cell.filter) !==
      JSON.stringify(this.props.cell.filter)
    ) {
      this.setState(
        {
          filter: JSON.stringify(this.props.cell.filter),
          filterInProgress: JSON.stringify(this.props.cell.filter),
          data: [],
          entities: [],
          hasData: false,
          lastUpdate: null,
          warning: "",
          error: "",
        },
        () => {
          this.seen?.clear();
          const cleared = {};
          for (const [key, val] of Object.entries(this.state)) {
            if (val && typeof val === "object" && "count" in val)
              cleared[key] = undefined;
          }
          this.setState(cleared);
          this.setState({
            key: this.props.cell.filter.key || this.state.key,
            titleKey:
              this.props.cell.filter.title_key ||
              this.props.cell.filter.key ||
              this.state.titleKey,
          });
          this.connect();
        },
      );
    }
  }
  componentWillUnmount() {
    rpc.removeFilter(this.uuid);
  }
  connect() {
    if (!this.props.cell.filter) {
      this.setState({ configOn: true });
      return;
    }
    rpc.addFilter(
      this.uuid,
      JSON.stringify(this.props.cell.filter),
      (message) => this.onMessage(message),
      (status) => this.setState(status),
    );
  }
  updateConfig() {
    if (this.state.err) return;
    try {
      const filter = JSON.parse(this.state.filterInProgress);
      if (!filter.query || !["metric", "composite"].includes(filter.data_type))
        throw Error("Choose a valid data type and query");
      if (filter.data_type === "metric" && !filter.query.key)
        throw Error("Metric key is required");
      if (
        filter.agg &&
        (!filter.agg.agg_field ||
          !filter.agg.time_field ||
          filter.agg.time_window_seconds <= 0)
      )
        throw Error(
          "Aggregation requires numeric field, time field, and positive window",
        );
      this.setState({ configOn: false, err: "" });
      store.dispatch(updateCell({ uuid: this.cell.uuid, filter }));
    } catch (e) {
      this.setState({ err: e.message });
    }
  }
  closeCell() {
    rememberRemoved(this.props.cell);
    store.dispatch(deleteCell(this.uuid));
  }
  updateTitle(title) {
    store.dispatch(updateCell({ uuid: this.uuid, title }));
  }
  renderError() {
    return this.state.err && <p role="alert">{this.state.err}</p>;
  }
  renderLayout() {
    const current = this.props.cell.attributes || { x: 0, y: 0, w: 36, h: 10 };
    const change = (key, delta) => {
      const next = { ...current, [key]: current[key] + delta };
      next.w = Math.max(12, Math.min(72, next.w));
      next.h = Math.max(8, next.h);
      next.x = Math.max(0, Math.min(72 - next.w, next.x));
      next.y = Math.max(0, next.y);
      store.dispatch(updateCell({ uuid: this.uuid, attributes: next }));
    };
    return (
      <details>
        <summary>Desktop layout controls</summary>
        <p>
          Move and resize this widget using keyboard-accessible buttons. Changes
          affect the saved desktop layout.
        </p>
        {[
          ["x", -6, "Move left"],
          ["x", 6, "Move right"],
          ["y", -2, "Move up"],
          ["y", 2, "Move down"],
          ["w", -6, "Narrower"],
          ["w", 6, "Wider"],
          ["h", -2, "Shorter"],
          ["h", 2, "Taller"],
        ].map(([key, delta, label]) => (
          <button key={label} onClick={() => change(key, delta)}>
            {label}
          </button>
        ))}
      </details>
    );
  }
  renderConfig() {
    return (
      <>
        <header className="widget-header cell-header">
          <ClickableText
            value={this.props.cell.title || "Untitled widget"}
            onChange={(title) => this.updateTitle(title)}
          />
          <button
            className="icon-button"
            aria-label="Settings"
            title="Settings"
            onClick={() =>
              this.setState({
                configOn: true,
                filterInProgress: this.state.filter,
              })
            }
          >
            <img src="/gear.png" alt="" />
          </button>
          <button
            className="icon-button"
            title="Remove"
            aria-label={`Remove ${this.props.cell.title || "widget"}`}
            onClick={() => this.closeCell()}
          >
            <img src="/closeCell.png" alt="" />
          </button>
        </header>
        {this.state.configOn ? (
          <section aria-label="Widget settings">
            {this.renderQueryBuilder()}
            {this.renderError()}
            {this.renderLayout()}
            <button onClick={() => this.updateConfig()}>
              Apply widget settings
            </button>
            <button
              onClick={() =>
                this.setState({
                  configOn: false,
                  err: "",
                  filterInProgress: this.state.filter,
                })
              }
            >
              Cancel
            </button>
            <p>Apply changes here, then save the dashboard to keep them.</p>
          </section>
        ) : (
          <div
            className={`widget-status ${this.state.connection === "Connected" && !this.state.error && !this.state.warning ? "quiet-status" : ""}`}
            role="status"
          >
            {this.state.error || this.state.connection || "Not connected"}
            {this.state.loading
              ? " · Loading history…"
              : !this.state.hasData
                ? " · No matching data"
                : ""}
            {this.state.lastUpdate &&
              ` · Updated ${new Date(this.state.lastUpdate).toLocaleTimeString()}`}
            {this.state.warning && <p>{this.state.warning}</p>}
            <button onClick={() => this.connect()}>Retry</button>
          </div>
        )}
      </>
    );
  }
}
